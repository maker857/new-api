# Seedance OpenAI Compatibility and Native Video API Design

## Goals

1. Keep `POST /v1/videos` as the OpenAI-compatible entry point.
2. For Doubao Seedance task channels, translate client `width` and `height` into Volcengine's `resolution` and `ratio` fields.
3. Forward compatible Seedance options such as `duration` and `seed` rather than silently dropping them.
4. Add the native Volcengine video task endpoint `POST /api/plan/v3/contents/generations/tasks`.

## Compatibility endpoint

`/v1/videos` continues to accept its existing OpenAI-style request body. When the selected task adaptor is Doubao Seedance:

- `width` and `height` are converted only when the client did not provide native `resolution` or `ratio` values.
- Supported exact dimensions map to a native resolution tier and aspect ratio. For example, `720x1280` becomes `720p` and `9:16`.
- Explicit native `resolution` and `ratio` supplied through compatible task metadata take precedence over inferred dimensions.
- The numeric `duration` field is forwarded as native `duration`.
- Other top-level options supported by the native request payload, such as `seed`, are preserved.

## Native endpoint

The native route accepts Volcengine's task creation JSON body without OpenAI width/height conversion:

```text
POST /api/plan/v3/contents/generations/tasks
```

The client selects the New API token with `Authorization: Bearer ...`; channel credentials remain server-side. The body `model` selects the Doubao video channel. The route forwards Volcengine-native options and returns the native upstream creation response shape.

## Scope and safeguards

- Native request fields are not interpreted as OpenAI fields.
- Channel selection and task persistence remain in New API so polling and task settlement retain channel affinity.
- Unknown parameters are preserved for the native endpoint, subject to the existing request body limit.
- The compatibility endpoint only derives dimensions for recognized exact size pairs; unsupported dimensions are rejected instead of guessing a ratio or resolution.
- Tests cover duration forwarding, `720x1280` conversion, explicit native parameter precedence, and native route registration/forwarding.
