import { useState } from "react";
import { Trash2, ChevronDown, ChevronUp, Plus } from "lucide-react";
import { useMemories, useDeleteMemory, useCreateMemory, useProjects } from "@/api/hooks";
import { formatDate, truncate } from "@/lib/utils";

export default function MemoryExplorer() {
  const [project, setProject] = useState("");
  const [expanded, setExpanded] = useState<string | null>(null);
  const [showAdd, setShowAdd] = useState(false);
  const [newContent, setNewContent] = useState("");
  const [newImportance, setNewImportance] = useState(0.5);
  const [newTags, setNewTags] = useState("");

  const { data, isLoading, refetch } = useMemories({ project: project || undefined, limit: 50 });
  const deleteMut = useDeleteMemory();
  const createMut = useCreateMemory();
  const { data: projs } = useProjects();

  const handleDelete = async (id: string) => {
    if (!confirm("Delete this memory?")) return;
    await deleteMut.mutateAsync(id);
  };

  const handleCreate = async () => {
    if (!newContent.trim()) return;
    await createMut.mutateAsync({
      content: newContent,
      project: project || undefined,
      importance: newImportance,
      tags: newTags
        .split(",")
        .map((t) => t.trim())
        .filter(Boolean),
    });
    setNewContent("");
    setNewTags("");
    setShowAdd(false);
    refetch();
  };

  return (
    <div className="p-6 max-w-7xl space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Memory Explorer</h1>
        <div className="flex items-center gap-3">
          <select
            value={project}
            onChange={(e) => setProject(e.target.value)}
            className="bg-surface border border-border rounded-lg px-3 py-2 text-sm text-text"
          >
            <option value="">All projects</option>
            {projs?.projects?.map((p) => (
              <option key={p.slug} value={p.slug}>
                {p.display_name}
              </option>
            ))}
          </select>
          <button
            onClick={() => setShowAdd(!showAdd)}
            className="flex items-center gap-1 px-3 py-2 rounded-lg bg-primary text-white text-sm hover:bg-primary-hover transition-colors"
          >
            <Plus className="w-4 h-4" /> Add Memory
          </button>
        </div>
      </div>

      {showAdd && (
        <div className="bg-surface border border-border rounded-xl p-4 space-y-3">
          <textarea
            value={newContent}
            onChange={(e) => setNewContent(e.target.value)}
            placeholder="Memory content..."
            rows={3}
            className="w-full bg-surface-2 border border-border rounded-lg p-3 text-sm text-text resize-none focus:outline-none focus:border-primary"
          />
          <div className="flex gap-3">
            <div className="flex items-center gap-2">
              <label className="text-xs text-text-muted">Importance:</label>
              <input
                type="range"
                min={0}
                max={1}
                step={0.1}
                value={newImportance}
                onChange={(e) => setNewImportance(Number(e.target.value))}
                className="w-24"
              />
              <span className="text-xs font-mono">{newImportance.toFixed(1)}</span>
            </div>
            <input
              value={newTags}
              onChange={(e) => setNewTags(e.target.value)}
              placeholder="Tags (comma-separated)"
              className="flex-1 bg-surface-2 border border-border rounded-lg px-3 py-1.5 text-sm text-text focus:outline-none focus:border-primary"
            />
            <button
              onClick={handleCreate}
              disabled={createMut.isPending}
              className="px-4 py-1.5 rounded-lg bg-accent text-bg text-sm font-medium hover:opacity-90 disabled:opacity-50"
            >
              {createMut.isPending ? "Saving..." : "Save"}
            </button>
          </div>
        </div>
      )}

      <div className="text-sm text-text-muted">{data?.total ?? 0} memories total</div>

      {isLoading ? (
        <p className="text-text-muted">Loading...</p>
      ) : (
        <div className="space-y-2">
          {data?.memories?.map((m) => (
            <div
              key={m.id}
              className="bg-surface border border-border rounded-xl overflow-hidden hover:border-border-hover transition-colors"
            >
              <div
                className="flex items-start gap-3 p-4 cursor-pointer"
                onClick={() => setExpanded(expanded === m.id ? null : m.id)}
              >
                <span className="shrink-0 mt-0.5 px-2 py-0.5 rounded text-[10px] font-mono uppercase bg-primary/15 text-primary">
                  {m.tier}
                </span>
                <div className="flex-1 min-w-0">
                  <p className="text-sm">{expanded === m.id ? m.content : truncate(m.content, 120)}</p>
                  <div className="flex gap-3 mt-1 text-xs text-text-muted">
                    <span className="font-mono">{m.importance.toFixed(2)}</span>
                    <span>{m.token_count} tok</span>
                    <span>{formatDate(m.created_at)}</span>
                    {m.tags?.map((t) => (
                      <span key={t} className="text-accent">
                        #{t}
                      </span>
                    ))}
                  </div>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      handleDelete(m.id);
                    }}
                    className="p-1.5 rounded-lg text-text-muted hover:text-danger hover:bg-danger/10 transition-colors"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                  {expanded === m.id ? (
                    <ChevronUp className="w-4 h-4 text-text-muted" />
                  ) : (
                    <ChevronDown className="w-4 h-4 text-text-muted" />
                  )}
                </div>
              </div>

              {expanded === m.id && (
                <div className="px-4 pb-4 border-t border-border pt-3">
                  <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-xs">
                    <div>
                      <span className="text-text-muted">ID</span>
                      <p className="font-mono text-text truncate">{m.id}</p>
                    </div>
                    <div>
                      <span className="text-text-muted">Agent</span>
                      <p className="text-text">{m.agent_id}</p>
                    </div>
                    <div>
                      <span className="text-text-muted">Tokens</span>
                      <p className="text-text">{m.token_count}</p>
                    </div>
                    <div>
                      <span className="text-text-muted">Project</span>
                      <p className="text-text">{m.project_id || "global"}</p>
                    </div>
                  </div>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
