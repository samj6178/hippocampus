## LinkedIn Post (English version)

---

I built an immune system for AI coding agents.

The problem: AI agents have no memory. Every session starts from zero. A bug you fixed yesterday? The agent will happily reintroduce it today.

The solution: Hippocampus MOS — an autonomous learning layer that sits between your codebase and any AI agent (Claude, Cursor, Copilot via MCP).

How it works:
1. Agent makes a mistake → system auto-captures the error
2. Similar errors cluster into prevention rules (WHEN/WATCH/DO format)
3. Next session: the system injects warnings BEFORE the agent touches risky code
4. At session end: git diff analysis checks if warnings actually prevented the bug

The results surprised me. A/B testing across 12 coding scenarios:
- Warning Precision: 86%
- Warning Recall: 100%
- Prevention Lift: +100% (all bugs prevented with warnings ON, all repeated without)

The system is fully local — Ollama for embeddings + LLM, PostgreSQL for memory, zero API costs. 33 MCP tools, 530+ tests, production-grade (graceful shutdown, health checks, concurrency limits, Prometheus metrics).

Named after the brain region responsible for memory consolidation. Built the 4-tier memory model (working → episodic → semantic → procedural) based on actual neuroscience — including prediction error encoding where surprising outcomes create stronger memories.

Tech stack: Go, PostgreSQL/TimescaleDB, Ollama (nomic-embed-text + qwen2.5:7b), MCP protocol.

Open source: github.com/[your-handle]/hippocampus

#AI #MCP #GoLang #DeveloperTools #OpenSource #AIAgents

---

## LinkedIn Post (Russian version)

---

Я создал иммунную систему для AI-агентов.

Проблема: AI-агенты не имеют памяти. Каждая сессия начинается с нуля. Баг, который ты пофиксил вчера? Агент с радостью повторит его завтра.

Решение: Hippocampus MOS — автономный обучающий слой между кодовой базой и любым AI-агентом (Claude, Cursor, Copilot через MCP).

Как работает:
1. Агент допускает ошибку → система автоматически захватывает её
2. Похожие ошибки кластеризуются в правила предотвращения
3. В следующей сессии: система вкидывает предупреждения ДО того, как агент тронет опасный код
4. В конце сессии: анализ git diff проверяет, сработали ли предупреждения

A/B тестирование по 12 сценариям:
- Precision предупреждений: 86%
- Recall: 100%
- Prevention Lift: +100% (все баги предотвращены с warnings ON)

Полностью локальная система — Ollama для embeddings + LLM, PostgreSQL для памяти, нулевые API-расходы. 33 MCP-инструмента, 530+ тестов, production-grade.

Назван в честь области мозга, отвечающей за консолидацию памяти. 4-уровневая модель памяти (рабочая → эпизодическая → семантическая → процедурная) построена на реальной нейронауке — включая кодирование ошибки предсказания, где неожиданные результаты создают более сильные воспоминания.

Стек: Go, PostgreSQL/TimescaleDB, Ollama, MCP protocol.

Open source: github.com/[your-handle]/hippocampus

#AI #MCP #GoLang #DeveloperTools #OpenSource #AIAgents
