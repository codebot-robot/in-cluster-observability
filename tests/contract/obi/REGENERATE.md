# Regenerating contract fixtures

This directory's fixtures freeze the OTLP→`capture.Event` translation. They protect us from silent regressions when the OBI sibling-container image is bumped. Per [`docs/design/obi-integration.md`](../../../docs/design/obi-integration.md) §6.

## Two kinds of fixtures

1. **Synthetic seed fixtures** (committed at v0.2 bootstrap): hand-built OTLP payloads that exercise the translator's known field set. Live under `testdata/translation/`. Useful for CI before real OBI integration is wired.
2. **Recorded real-OBI fixtures** (regenerated on each OBI image bump): captured OTLP request bodies from a real OBI sidecar running against a canary workload.

The harness treats both the same way — it doesn't know the source. The expectation is that recorded fixtures replace synthetic ones as soon as we have a working Kind-based regeneration pipeline.

## Regenerating synthetic seed fixtures

```sh
go test ./tests/contract/obi -seed -update
```

`-seed` enables the seed-fixture test (`TestSeedFixtures`) which writes the synthetic `input.binpb` / `kind` files. `-update` writes the matching `golden.json` from the current translator output.

## Regenerating recorded OBI fixtures (per OBI image bump)

Pre-requisites: Kind, kubectl, Docker, the pinned OBI image (`ghcr.io/open-telemetry/obi:<tag>`).

1. **Stand up a recording cluster**:
   ```sh
   kind create cluster --name ollie-fixture-recorder
   ```
2. **Run a packet-tap OTLP sink** in the cluster. This is a small OTel collector configured to dump every received OTLP request as binary protobuf to a volume. Recipe lives in `tests/contract/obi/recorder/` (TODO — first version lands with the first real recording).
3. **Deploy OBI as a sidecar** alongside a canary workload (`nginx`, an httptest server, etc.), pointing OBI's OTLP exporter at the recording sink.
4. **Drive the canary** with a known traffic pattern (e.g. `wrk -t1 -c1 -d10s http://nginx`).
5. **Copy the recorded `.binpb` files** out of the recorder volume into `tests/contract/obi/testdata/<category>/<case>/input.binpb`. Set `kind` to `metrics` or `traces`.
6. **Regenerate goldens**:
   ```sh
   go test ./tests/contract/obi -update
   ```
7. **Commit the new fixtures + goldens** in the same PR as the OBI image-tag bump (per ADR-0010 / ADR-0018 single-bump-PR policy).

## Adding a new case

- Put the input under `testdata/<category>/<case>/input.binpb` plus a `kind` file.
- Run `go test ./tests/contract/obi -update` to generate the golden.
- Commit.
- The next non-`-update` run of `go test` will exercise the new case.

## When a contract test fails

- **Translation regression**: a code change in `pkg/capture/translate*.go` altered output. Either fix the code or regenerate goldens with `-update` and review the diff.
- **OBI schema change**: a new OBI image emits a slightly different OTLP shape. Recapture the fixtures from real OBI; if the new shape is intended, regenerate goldens. If unintended, file upstream.
- **Synthetic-vs-real divergence**: synthetic seed fixtures don't perfectly match real OBI output. Replace the case with a recorded version per the steps above.
