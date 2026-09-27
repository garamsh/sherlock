"""Shared interface for memory architectures."""
from __future__ import annotations
from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import Any


@dataclass
class RetrievalResult:
    turn_id: str
    role: str
    content: str
    score: float
    metadata: dict = field(default_factory=dict)


@dataclass
class StoreItem:
    turn_id: str
    role: str
    content: str
    metadata: dict = field(default_factory=dict)


class Memory(ABC):
    NAME: str = ""

    @abstractmethod
    def store(self, turn_id: str, role: str, content: str, metadata: dict[str, Any]) -> None: ...
    @abstractmethod
    def retrieve(self, query: str, k: int = 5) -> list[RetrievalResult]: ...
    @abstractmethod
    def stats(self) -> dict[str, Any]: ...


def get_now_ms() -> float:
    import time
    return time.perf_counter() * 1000.0
