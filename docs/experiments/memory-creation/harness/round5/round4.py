"""Round 4's unchanged dataset module, loaded by path.

Both rounds name a module `dataset`, so an ordinary import inside round 5
resolves to round 5's own file.
"""
from __future__ import annotations
import importlib.util
import os

ROUND4 = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "round4")


def _load(name):
    spec = importlib.util.spec_from_file_location(
        f"round4_{name}", os.path.join(ROUND4, f"{name}.py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


dataset = _load("dataset")
