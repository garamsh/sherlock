# Playwright script tool

Browser-automation tool for the platform: a Python script run through the universal script adapter (`internal/tool/script`), registered via the config's `tools:` section.

## Files

- `browser_tool.py` — the tool. Reads a JSON job on stdin, drives a headless Chromium, prints a JSON result on stdout.
- `schema.json` — the input and output JSON Schemas, side by side under the `input` and `output` keys (same format as the built-in Go tools).

## Install

```
pip install playwright
playwright install chromium
```

The script runs without Playwright installed: it answers `{"ok": false, ..., "error": "playwright not installed"}` instead of crashing, so the platform stays up and the model gets a structured error.

## Run standalone

```
echo '{"url": "https://example.com", "action": "screenshot"}' | python3 browser_tool.py
```

Input and output shapes are defined in `schema.json`. The four actions:

- `screenshot` — saves a screenshot to `/tmp/screenshot.png`, returns its path.
- `extract_text` — returns the page's `document.body.innerText`.
- `click` — clicks `selector`.
- `fill_form` — fills `selector` with `text`.

`timeout_seconds` (1–300, default 30) caps navigation and each Playwright operation. Every result carries all four output keys (`ok`, `screenshot_path`, `extracted_text`, `error`); failures return `ok: false` with the exception message in `error` and exit zero, so the adapter surfaces the structured error instead of a process failure.

## Register with the platform

The repository-root `gagent.yaml` registers the tool:

```yaml
tools:
  - name: playwright
    description: "Browser automation via Playwright"
    input_schema: tools/playwright/schema.json
    output_schema: tools/playwright/schema.json
    exec:
      command: ["python3", "tools/playwright/browser_tool.py"]
      stdin_format: "json"
      stdout_format: "json"
      timeout_seconds: 30
```

Schema and command paths are relative to the platform's working directory, so run the platform from the repository root. The tool then appears in every environment's registry alongside the built-ins.

## Add another script tool

1. Write an executable script that reads a JSON document on stdin and writes a JSON document on stdout.
2. Add a `schema.json` next to it with the input and output schemas.
3. Add an entry to the `tools:` section of `gagent.yaml` pointing at the script and schemas.

No Go changes are needed: the adapter (`internal/tool/script`) runs any command, and the gateway registers every `tools:` entry into each environment.
