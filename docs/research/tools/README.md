# Research Tools

This directory holds utilities with a stable, documented interface and a credible use across future studies.

No current utility qualifies. The architecture metrics expect that study's query and result shapes; the result renderers, claim checkers, model-call cache, and mutation runner depend on their own studies' fixtures, reports, or target packages. Keep those harnesses beside their reports. Move a utility here when its inputs and outputs can serve other studies without embedding one study's assumptions, and document how to invoke it.
