"""Embed texts with the local open model; run under uv with fastembed pinned (see README)."""
import json
import sys
import time

from fastembed import TextEmbedding

MODEL = 'BAAI/bge-base-en-v1.5'
# The model card's retrieval instruction for short queries against passages.
QUERY_PREFIX = 'Represent this sentence for searching relevant passages: '


def main(source, target):
    request = json.load(open(source))
    model = TextEmbedding(MODEL)
    texts = [(QUERY_PREFIX if kind == 'query' else '') + text for kind, text in request['items']]
    started = time.monotonic()
    vectors = [[round(float(x), 6) for x in v] for v in model.embed(texts, batch_size=32)]
    json.dump({'model': MODEL, 'seconds': time.monotonic() - started, 'vectors': vectors}, open(target, 'w'))


if __name__ == '__main__':
    main(sys.argv[1], sys.argv[2])
