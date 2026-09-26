"""Build the #888 read-time variants corpus: #885's generator
(../confirm/corpus.py) with its family sizes, rules and selection, from
seed 888.

    python3 docs/research/chained-recall/readtime/corpus.py assemble   # -> candidates.json and corpus.json
    python3 docs/research/chained-recall/readtime/corpus.py select     # candidates + check + L's offline records -> corpus.json

Every step and file is as ../confirm/corpus.py describes it, read and
written beside this file.
"""

import importlib.util
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent


def _confirm_corpus():
    spec = importlib.util.spec_from_file_location("chained_recall_confirm_corpus", HERE.parent / "confirm" / "corpus.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


confirm = _confirm_corpus()
confirm.SEED = 888
confirm.HERE = HERE

if __name__ == "__main__":
    {"assemble": confirm.assemble, "select": confirm.select}[sys.argv[1]]()
