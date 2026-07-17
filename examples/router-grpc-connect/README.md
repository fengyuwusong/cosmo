# Router ConnectRPC gRPC subgraph

This example runs the projects gRPC subgraph over ConnectRPC and configures the Router to use protobuf encoding.

From this directory, run:

```bash
./start.sh
```

The script starts the projects service on `http://localhost:4011`, composes `config.json`, downloads a Router binary when needed, and starts the Router on `http://localhost:3002`. The composition includes the related demo schemas required by Federation, but the query below only calls the projects service.

Open the playground and run:

```graphql
query {
  projects {
    id
    name
    status
  }
}
```

Change `connectrpc_encoding` in `config.yaml` to `json` to use ConnectRPC JSON encoding. The service accepts both encodings.
