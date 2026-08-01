#!/usr/bin/env python3
"""Playwright browser-automation script tool.

Reads a JSON job on stdin, runs it against a headless Chromium, and
prints a JSON result on stdout. The platform runs this script through
the universal adapter (internal/tool/script); see README.md for install
and registration.
"""

import json
import sys

SCREENSHOT_PATH = "/tmp/screenshot.png"


def result(ok, screenshot_path=None, extracted_text=None, error=None):
    """Build the output document; all four keys are always present."""
    return {
        "ok": ok,
        "screenshot_path": screenshot_path,
        "extracted_text": extracted_text,
        "error": error,
    }


def run(job):
    """Execute one job and return the output document."""
    from playwright.sync_api import sync_playwright

    url = job["url"]
    action = job["action"]
    timeout_ms = int(job.get("timeout_seconds", 30)) * 1000

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        try:
            page = browser.new_page()
            page.set_default_timeout(timeout_ms)
            page.goto(url, timeout=timeout_ms)
            if action == "screenshot":
                page.screenshot(path=SCREENSHOT_PATH)
                return result(True, screenshot_path=SCREENSHOT_PATH)
            if action == "extract_text":
                text = page.evaluate("document.body.innerText")
                return result(True, extracted_text=text)
            if action == "click":
                page.click(job["selector"])
                return result(True)
            if action == "fill_form":
                page.fill(job["selector"], job.get("text", ""))
                return result(True)
            return result(False, error=f"unknown action: {action}")
        finally:
            browser.close()


def main():
    try:
        job = json.loads(sys.stdin.read())
    except json.JSONDecodeError as e:
        print(json.dumps(result(False, error=f"invalid JSON input: {e}")))
        return
    try:
        out = run(job)
    except ImportError:
        out = result(False, error="playwright not installed")
    except Exception as e:  # navigation, selector, and timeout errors
        out = result(False, error=str(e))
    print(json.dumps(out))


if __name__ == "__main__":
    main()
