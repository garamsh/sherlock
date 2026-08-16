"""The thirty-eight candidate architectures, one file per series letter.

The runners select an architecture by name off this module, so this file is the
surface they see and the series files are where the code lives. A reader looking
for one architecture opens the file its identifier names — `g.py` for G6, `m.py`
for M3 — rather than reading the whole set.

  a.py  A0-A2  baselines            f.py  F1-F5  storage discipline
  b.py  B1-B3  classic retrieval    g.py  G1-G9  research-inspired
  c.py  C3     decay on retrieval   h.py  H1-H6  explicit hybrids
  d.py  D2     curation             m.py  M1-M6  pure structures
  e.py  E1-E4  cognition

`similarity.py` holds the tokenizer and the two cosines every series shares.
"""
from .a import NoMemory, AutoCaptureUniform, FullTextLog
from .b import KeywordOnly, SemanticOnly, HybridRAG
from .c import EbbinghausDecay
from .d import CurationOnly
from .e import (TwoTierBasic, TwoTierReconsolidation, TwoTierSalience,
                FullCognitive)
from .f import (SalienceTieredEbbinghaus, Supersession, Compression,
                TieredRetention, Combined)
from .g import (GenerativeAgentsStyle, EbbinghausInspired, HierarchicalMemory,
                ReflectionBased, CognitiveIndex, DecadeStable,
                RecursiveRetrieval, RecursiveRetrieval3Hop, SpreadingActivation)
from .h import (H1G6G4, H2G6G7, H3G4G7, H4F5G4, H5EpisodicSemantic,
                H6TripleHybrid)
from .m import (HopfieldAssociative, KanervaSDM, HNSWMemory, LSHMemory,
                WorkingMemoryLru, LsmMemory)
from .similarity import tokenize, hash_vector, cosine, tfidf_cosine

__all__ = [
    "NoMemory", "AutoCaptureUniform", "FullTextLog",
    "KeywordOnly", "SemanticOnly", "HybridRAG",
    "EbbinghausDecay",
    "CurationOnly",
    "TwoTierBasic", "TwoTierReconsolidation", "TwoTierSalience", "FullCognitive",
    "SalienceTieredEbbinghaus", "Supersession", "Compression", "TieredRetention",
    "Combined",
    "GenerativeAgentsStyle", "EbbinghausInspired", "HierarchicalMemory",
    "ReflectionBased", "CognitiveIndex", "DecadeStable", "RecursiveRetrieval",
    "RecursiveRetrieval3Hop", "SpreadingActivation",
    "H1G6G4", "H2G6G7", "H3G4G7", "H4F5G4", "H5EpisodicSemantic", "H6TripleHybrid",
    "HopfieldAssociative", "KanervaSDM", "HNSWMemory", "LSHMemory",
    "WorkingMemoryLru", "LsmMemory",
    "tokenize", "hash_vector", "cosine", "tfidf_cosine",
]
