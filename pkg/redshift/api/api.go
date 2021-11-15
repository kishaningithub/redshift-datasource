package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/redshiftdataapiservice"
	"github.com/aws/aws-sdk-go/service/redshiftdataapiservice/redshiftdataapiserviceiface"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/aws/aws-sdk-go/service/secretsmanager/secretsmanageriface"
	"github.com/grafana/grafana-aws-sdk/pkg/awsds"
	"github.com/grafana/grafana-aws-sdk/pkg/sql/api"
	awsModels "github.com/grafana/grafana-aws-sdk/pkg/sql/models"
	"github.com/grafana/redshift-datasource/pkg/redshift/models"
	"github.com/grafana/sqlds/v2"
)

type API struct {
	Client        redshiftdataapiserviceiface.RedshiftDataAPIServiceAPI
	SecretsClient secretsmanageriface.SecretsManagerAPI
	settings      *models.RedshiftDataSourceSettings
}

func New(sessionCache *awsds.SessionCache, settings awsModels.Settings) (api.AWSAPI, error) {
	redshiftSettings := settings.(*models.RedshiftDataSourceSettings)
	sess, err := awsds.GetSessionWithDefaultRegion(sessionCache, redshiftSettings.AWSDatasourceSettings)
	if err != nil {
		return nil, err
	}

	svc := redshiftdataapiservice.New(sess)
	svc.Handlers.Send.PushFront(func(r *request.Request) {
		r.HTTPRequest.Header.Set("User-Agent", awsds.GetUserAgentString("Redshift"))
	})
	secretsSVC := secretsmanager.New(sess)
	secretsSVC.Handlers.Send.PushFront(func(r *request.Request) {
		r.HTTPRequest.Header.Set("User-Agent", awsds.GetUserAgentString("Redshift"))
	})
	return &API{
		Client:        svc,
		SecretsClient: secretsSVC,
		settings:      redshiftSettings,
	}, nil
}

type apiInput struct {
	ClusterIdentifier *string
	Database          *string
	DbUser            *string
	SecretARN         *string
}

func (c *API) apiInput() apiInput {
	res := apiInput{
		ClusterIdentifier: aws.String(c.settings.ClusterIdentifier),
		Database:          aws.String(c.settings.Database),
	}
	if c.settings.UseManagedSecret {
		res.SecretARN = aws.String(c.settings.ManagedSecret.ARN)
	} else {
		res.DbUser = aws.String(c.settings.DBUser)
	}
	return res
}

func (c *API) Execute(ctx context.Context, input *api.ExecuteQueryInput) (*api.ExecuteQueryOutput, error) {
	commonInput := c.apiInput()
	redshiftInput := &redshiftdataapiservice.ExecuteStatementInput{
		ClusterIdentifier: commonInput.ClusterIdentifier,
		Database:          commonInput.Database,
		DbUser:            commonInput.DbUser,
		SecretArn:         commonInput.SecretARN,
		Sql:               aws.String(input.Query),
	}

	output, err := c.Client.ExecuteStatementWithContext(ctx, redshiftInput)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", api.ExecuteError, err)
	}

	return &api.ExecuteQueryOutput{ID: *output.Id}, nil
}

func (c *API) Status(ctx aws.Context, output *api.ExecuteQueryOutput) (*api.ExecuteQueryStatus, error) {
	statusResp, err := c.Client.DescribeStatementWithContext(ctx, &redshiftdataapiservice.DescribeStatementInput{
		Id: aws.String(output.ID),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", api.StatusError, err)
	}

	var finished bool
	state := *statusResp.Status
	switch state {
	case redshiftdataapiservice.StatusStringFailed,
		redshiftdataapiservice.StatusStringAborted:
		finished = true
		err = errors.New(*statusResp.Error)
	case redshiftdataapiservice.StatusStringFinished:
		finished = true
	default:
		finished = false
	}

	return &api.ExecuteQueryStatus{
		ID:       output.ID,
		State:    state,
		Finished: finished,
	}, err
}

func (c *API) Stop(output *api.ExecuteQueryOutput) error {
	_, err := c.Client.CancelStatement(&redshiftdataapiservice.CancelStatementInput{
		Id: &output.ID,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", err, api.StopError)
	}
	return nil
}

func (c *API) Regions(aws.Context) ([]string, error) {
	// TBD
	return []string{}, nil
}

func (c *API) Databases(ctx aws.Context, options sqlds.Options) ([]string, error) {
	// TBD
	return []string{}, nil
}

func (c *API) Schemas(ctx aws.Context, options sqlds.Options) ([]string, error) {
	commonInput := c.apiInput()
	input := &redshiftdataapiservice.ListSchemasInput{
		ClusterIdentifier: commonInput.ClusterIdentifier,
		Database:          commonInput.Database,
		DbUser:            commonInput.DbUser,
		SecretArn:         commonInput.SecretARN,
	}
	isFinished := false
	res := []string{}
	for !isFinished {
		out, err := c.Client.ListSchemasWithContext(ctx, input)
		if err != nil {
			return nil, err
		}
		input.NextToken = out.NextToken
		for _, sc := range out.Schemas {
			if sc != nil {
				res = append(res, *sc)
			}
		}
		if input.NextToken == nil {
			isFinished = true
		}
	}
	return res, nil
}

func (c *API) Tables(ctx aws.Context, options sqlds.Options) ([]string, error) {
	schema := options["schema"]
	// We use the "public" schema by default if not specified
	if schema == "" {
		schema = "public"
	}
	commonInput := c.apiInput()
	input := &redshiftdataapiservice.ListTablesInput{
		ClusterIdentifier: commonInput.ClusterIdentifier,
		Database:          commonInput.Database,
		DbUser:            commonInput.DbUser,
		SecretArn:         commonInput.SecretARN,
		SchemaPattern:     aws.String(schema),
	}
	isFinished := false
	res := []string{}
	for !isFinished {
		out, err := c.Client.ListTablesWithContext(ctx, input)
		if err != nil {
			return nil, err
		}
		input.NextToken = out.NextToken
		for _, t := range out.Tables {
			if t.Name != nil {
				res = append(res, *t.Name)
			}
		}
		if input.NextToken == nil {
			isFinished = true
		}
	}
	return res, nil
}

func (c *API) Columns(ctx aws.Context, options sqlds.Options) ([]string, error) {
	schema, table := options["schema"], options["table"]
	commonInput := c.apiInput()
	input := &redshiftdataapiservice.DescribeTableInput{
		ClusterIdentifier: commonInput.ClusterIdentifier,
		Database:          commonInput.Database,
		DbUser:            commonInput.DbUser,
		SecretArn:         commonInput.SecretARN,
		Schema:            aws.String(schema),
		Table:             aws.String(table),
	}
	isFinished := false
	res := []string{}
	for !isFinished {
		out, err := c.Client.DescribeTableWithContext(ctx, input)
		if err != nil {
			return nil, err
		}
		input.NextToken = out.NextToken
		for _, c := range out.ColumnList {
			if c.Name != nil {
				res = append(res, *c.Name)
			}
		}
		if input.NextToken == nil {
			isFinished = true
		}
	}
	return res, nil
}

func (c *API) Secrets(ctx aws.Context) ([]models.ManagedSecret, error) {
	input := &secretsmanager.ListSecretsInput{
		Filters: []*secretsmanager.Filter{
			{
				// Only secrets with the tag RedshiftQueryOwner can be used
				// https://docs.aws.amazon.com/redshift/latest/mgmt/query-editor.html#query-cluster-configure
				Key:    aws.String(secretsmanager.FilterNameStringTypeTagKey),
				Values: []*string{aws.String("RedshiftQueryOwner")},
			},
		},
	}
	isFinished := false
	redshiftSecrets := []models.ManagedSecret{}
	for !isFinished {
		out, err := c.SecretsClient.ListSecretsWithContext(ctx, input)
		if err != nil {
			return nil, err
		}
		input.NextToken = out.NextToken
		if input.NextToken == nil {
			isFinished = true
		}
		for _, s := range out.SecretList {
			if s.ARN == nil || s.Name == nil {
				continue
			}
			redshiftSecrets = append(redshiftSecrets, models.ManagedSecret{
				ARN:  *s.ARN,
				Name: *s.Name,
			})
		}
	}
	return redshiftSecrets, nil
}

func (c *API) Secret(ctx aws.Context, options sqlds.Options) (*models.RedshiftSecret, error) {
	arn := options["secretARN"]
	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(arn),
	}
	out, err := c.SecretsClient.GetSecretValueWithContext(ctx, input)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, fmt.Errorf("missing secret content")
	}
	res := &models.RedshiftSecret{}
	err = json.Unmarshal([]byte(*out.SecretString), res)
	if err != nil {
		return nil, err
	}
	return res, nil
}
