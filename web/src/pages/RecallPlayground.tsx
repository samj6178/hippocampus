import { useState } from "react";
import { Search, Loader2 } from "lucide-react";
import { Card } from "@/components/Card";
import { useRecall, useProjects } from "@/api/hooks";
import type { RecallResponse } from "@/api/client";
import { cn } from "@/lib/utils";

export default function RecallPlayground() {
  const [query, setQuery] = useState("");
  const [project, setProject] = useState("");
  const [budget, setBudget] = useState(4096);
  const [result, setResult] = useState<RecallResponse | null>(null);
  const recallMut = useRecall();
  const { data: projs } = useProjects();

  const handleRecall = async () => {
    if (!query.trim()) return;
    const res = await recallMut.mutateAsync({
      query,
      project: project || undefined,
      budget_tokens: budget,
    });
    setResult(res);
  };

  return (
    <div className="p-6 max-w-5xl space-y-6">
      <h1 className="text-2xl font-bold">Recall Playground</h1>

      <Card>
        <div className="space-y-4">
          <div className="flex gap-3">
            <div className="flex-1 relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-text-muted" />
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleRecall()}
                placeholder="What do you need to recall? Describe your task or question..."
                className="w-full bg-surface-2 border border-border rounded-lg pl-10 pr-4 py-3 text-sm text-text focus:outline-none focus:border-primary"
              />
            </div>
            <button
              onClick={handleRecall}
              disabled={recallMut.isPending || !query.trim()}
              className="px-5 py-3 rounded-lg bg-primary text-white text-sm font-medium hover:bg-primary-hover transition-colors disabled:opacity-50 flex items-center gap-2"
            >
              {recallMut.isPending ? <Loader2 className="w-4 h-4 animate-spin" /> : <Search className="w-4 h-4" />}
              Recall
            </button>
          </div>

          <div className="flex items-center gap-4 text-sm">
            <select
              value={project}
              onChange={(e) => setProject(e.target.value)}
              className="bg-surface-2 border border-border rounded-lg px-3 py-1.5 text-text"
            >
              <option value="">All projects</option>
              {projs?.projects?.map((p) => (
                <option key={p.slug} value={p.slug}>{p.display_name}</option>
              ))}
            </select>
            <div className="flex items-center gap-2">
              <label className="text-text-muted text-xs">Budget:</label>
              <input
                type="number"
                value={budget}
                onChange={(e) => setBudget(Number(e.target.value))}
                min={256}
                max={32768}
                step={256}
                className="w-24 bg-surface-2 border border-border rounded-lg px-2 py-1.5 text-text text-xs font-mono"
              />
              <span className="text-xs text-text-muted">tokens</span>
            </div>
          </div>
        </div>
      </Card>

      {result && (
        <div className="space-y-4">
          <div className="flex gap-4 text-sm text-text-muted">
            <span>Candidates: <strong className="text-text">{result.candidates_considered}</strong></span>
            <span>Sources: <strong className="text-text">{result.context.sources.length}</strong></span>
            <span>Tokens: <strong className="text-text">{result.context.token_count}</strong></span>
            <span>Confidence: <strong className="text-text">{(result.context.confidence * 100).toFixed(0)}%</strong></span>
          </div>

          <Card>
            <h2 className="text-lg font-semibold mb-3">Assembled Context</h2>
            <pre className="bg-surface-2 rounded-lg p-4 text-sm font-mono text-text whitespace-pre-wrap overflow-auto max-h-96 border border-border">
              {result.context.text || "(empty)"}
            </pre>
          </Card>

          <Card>
            <h2 className="text-lg font-semibold mb-3">Sources</h2>
            <div className="space-y-2">
              {result.context.sources.map((src, i) => (
                <div
                  key={i}
                  className="flex items-start gap-3 p-3 rounded-lg bg-surface-2 border border-border"
                >
                  <span
                    className={cn(
                      "shrink-0 mt-0.5 px-2 py-0.5 rounded text-[10px] font-mono uppercase",
                      src.tier === "episodic" && "bg-primary/15 text-primary",
                      src.tier === "semantic" && "bg-accent/15 text-accent",
                      src.tier === "procedural" && "bg-warning/15 text-warning"
                    )}
                  >
                    {src.tier}
                  </span>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm">{src.snippet}</p>
                    <div className="flex gap-3 mt-1 text-xs text-text-muted">
                      <span>relevance: {src.relevance.toFixed(3)}</span>
                      <span className="font-mono truncate">{src.memory_id}</span>
                    </div>
                  </div>
                  <div className="shrink-0">
                    <div
                      className="w-10 h-2 rounded-full bg-surface overflow-hidden"
                      title={`${(src.relevance * 100).toFixed(0)}%`}
                    >
                      <div
                        className="h-full bg-primary rounded-full"
                        style={{ width: `${Math.min(src.relevance * 100, 100)}%` }}
                      />
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </Card>
        </div>
      )}
    </div>
  );
}
