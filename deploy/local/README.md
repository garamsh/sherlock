# Local deployment (testing only)

Run the gagent platform in Docker against the host's Docker daemon. **Local testing only, on a trusted host: the gateway has no authentication** — anyone who can reach the port can drive the agent.

From this directory:

```sh
docker compose build
docker compose up -d
curl -X POST localhost:8080/envs/demo/messages -d '{"content":"hello"}'   # returns 202
```

The sample `gagent.yaml` uses the mock model provider, so the agent replies without any API key. `sandbox_exec` requires the environment image (`alpine:3.21`) on the host daemon. Agent replies land in the `gagent-data` volume under `/data/demo/outbox/`.
