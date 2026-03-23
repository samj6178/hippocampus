import { useMemories, useProjects } from "@/api/hooks";
import { formatDate, truncate, cn } from "@/lib/utils";
import { useState } from "react";

export default function Timeline() {
  const [project, setProject] = useState("");
  const { data, isLoading } = useMemories({ project: project || undefined, limit: 50 });
  const { data: projs } = useProjects();

  const memories = data?.memories ?? [];

  const groupedByDate: Record<string, typeof memories> = {};
  for (const m of memories) {
    const day = m.created_at.split("T")[0];
    if (!groupedByDate[day]) groupedByDate[day] = [];
    groupedByDate[day].push(m);
  }

  const sortedDays = Object.keys(groupedByDate).sort((a, b) => b.localeCompare(a));

  return (
    <div className="p-6 max-w-4xl space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Timeline</h1>
        <select
          value={project}
          onChange={(e) => setProject(e.target.value)}
          className="bg-surface border border-border rounded-lg px-3 py-2 text-sm text-text"
        >
          <option value="">All projects</option>
          {projs?.projects?.map((p) => (
            <option key={p.slug} value={p.slug}>{p.display_name}</option>
          ))}
        </select>
      </div>

      {isLoading ? (
        <p className="text-text-muted">Loading...</p>
      ) : sortedDays.length === 0 ? (
        <p className="text-text-muted text-sm">No memories to display</p>
      ) : (
        <div className="relative">
          <div className="absolute left-[19px] top-0 bottom-0 w-px bg-border" />

          {sortedDays.map((day) => (
            <div key={day} className="mb-8">
              <div className="flex items-center gap-3 mb-3 relative">
                <div className="w-10 h-10 rounded-full bg-primary/15 border-2 border-primary flex items-center justify-center z-10">
                  <span className="text-xs font-bold text-primary">
                    {new Date(day).getDate()}
                  </span>
                </div>
                <span className="text-sm font-semibold text-text">
                  {new Date(day).toLocaleDateString("ru-RU", { month: "long", year: "numeric", day: "numeric" })}
                </span>
                <span className="text-xs text-text-muted">
                  {groupedByDate[day].length} memories
                </span>
              </div>

              <div className="ml-12 space-y-2">
                {groupedByDate[day].map((m) => (
                  <div
                    key={m.id}
                    className={cn(
                      "relative bg-surface border border-border rounded-xl p-4",
                      "hover:border-border-hover transition-colors",
                      "before:absolute before:left-[-25px] before:top-5 before:w-3 before:h-px before:bg-border"
                    )}
                  >
                    <div className="flex items-start gap-2 mb-1">
                      <span className="px-2 py-0.5 rounded text-[10px] font-mono uppercase bg-primary/15 text-primary">
                        {m.tier}
                      </span>
                      <span className="text-xs text-text-muted">
                        {formatDate(m.created_at)}
                      </span>
                      <span className="text-xs font-mono text-text-muted ml-auto">
                        imp: {m.importance.toFixed(2)}
                      </span>
                    </div>
                    <p className="text-sm leading-relaxed">{truncate(m.content, 200)}</p>
                    {m.tags && m.tags.length > 0 && (
                      <div className="flex gap-2 mt-2">
                        {m.tags.map((t) => (
                          <span key={t} className="text-xs text-accent">#{t}</span>
                        ))}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
