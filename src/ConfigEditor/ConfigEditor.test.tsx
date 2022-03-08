import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import React from 'react';
import { select } from 'react-select-event';

import { mockDatasourceOptions } from '../__mocks__/datasource';
import { selectors } from '../selectors';
import { ConfigEditor } from './ConfigEditor';

const secret = { name: 'foo', arn: 'arn:foo' };
const clusterIdentifier = 'cluster';
const dbUser = 'username';
const secretFetched = { dbClusterIdentifier: clusterIdentifier, username: dbUser };
const cluster = { clusterIdentifier: 'foo', endpoint: { address: 'foo.a.b.c', port: 123 }, database: 'db' };

jest.mock('@grafana/aws-sdk', () => {
  return {
    ...(jest.requireActual('@grafana/aws-sdk') as any),
    ConnectionConfig: function ConnectionConfig() {
      return <></>;
    },
  };
});

jest.mock('@grafana/runtime', () => {
  return {
    ...(jest.requireActual('@grafana/runtime') as any),
    getBackendSrv: () => ({
      put: jest.fn().mockResolvedValue({ datasource: {} }),
      get: jest.fn().mockImplementation((url, args) => (url.includes('secrets') ? [secret] : [cluster])),
      post: jest.fn().mockResolvedValue(secretFetched),
    }),
  };
});

const props = mockDatasourceOptions;

describe('ConfigEditor', () => {
  it('should display temporary credentials by default', () => {
    render(<ConfigEditor {...props} />);
    expect(screen.getByText(selectors.components.ConfigEditor.ClusterID.input)).toBeInTheDocument();
    expect(screen.getByText(selectors.components.ConfigEditor.Database.input)).toBeInTheDocument();
    expect(screen.getByText(selectors.components.ConfigEditor.DatabaseUser.input)).toBeInTheDocument();
  });

  it('should switch to use the Secret Manager', () => {
    render(<ConfigEditor {...props} />);
    screen.getByText('AWS Secrets Manager').click();
    expect(screen.getByText(selectors.components.ConfigEditor.ManagedSecret.input)).toBeInTheDocument();
    expect(screen.getByText(selectors.components.ConfigEditor.ClusterID.input)).toBeInTheDocument();
    expect(screen.getByText(selectors.components.ConfigEditor.Database.input)).toBeInTheDocument();
  });

  it('should select a secret', async () => {
    const onChange = jest.fn();
    render(<ConfigEditor {...props} onOptionsChange={onChange} />);

    screen.getByText('AWS Secrets Manager').click();

    const selectEl = screen.getByLabelText(selectors.components.ConfigEditor.ManagedSecret.input);
    expect(selectEl).toBeInTheDocument();
    await select(selectEl, secret.arn, { container: document.body });

    expect(onChange).toHaveBeenCalledWith({
      ...props.options,
      jsonData: { ...props.options.jsonData, managedSecret: secret },
    });
  });

  it('should allow user to enter a database', async () => {
    const onChange = jest.fn();
    render(<ConfigEditor {...props} onOptionsChange={onChange} />);

    const dbField = screen.getByTestId('data-testid database');
    expect(dbField).toBeInTheDocument();
    fireEvent.change(dbField, { target: { value: 'abcd' } });

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith({
      ...props.options,
      jsonData: { ...props.options.jsonData, database: 'abcd' },
    });
  });

  it('should populate the `url` prop when clusterIdentifier is selected', async () => {
    const onChange = jest.fn();
    const propsWithDB = props;
    propsWithDB.options.jsonData.database = 'test-db';
    render(<ConfigEditor {...props} onOptionsChange={onChange} />);

    const selectEl = screen.getByLabelText(selectors.components.ConfigEditor.ClusterID.input);
    expect(selectEl).toBeInTheDocument();
    await select(selectEl, cluster.clusterIdentifier, { container: document.body });

    await waitFor(() => expect(onChange).toHaveBeenCalledTimes(1));
    expect(onChange).toHaveBeenCalledWith({
      ...props.options,
      url: 'foo.a.b.c:123/test-db',
      jsonData: { ...props.options.jsonData, clusterIdentifier: 'foo' },
    });
  });

  it('should show the cluster identifier and the db user', async () => {
    const onChange = jest.fn();
    render(
      <ConfigEditor
        {...props}
        onOptionsChange={onChange}
        // setting the managedSecret will trigger the secret retrieval
        options={{
          ...props.options,
          jsonData: { ...props.options.jsonData, useManagedSecret: true, managedSecret: secret },
        }}
      />
    );
    // the dbUser and clusterIdentifier update is delegated to the onChange function
    await waitFor(() =>
      expect(onChange).toHaveBeenCalledWith({
        ...props.options,
        jsonData: {
          ...props.options.jsonData,
          dbUser,
          useManagedSecret: true,
          managedSecret: secret,
          clusterIdentifier,
        },
      })
    );
  });
});
