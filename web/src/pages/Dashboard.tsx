import { Database, Cpu, HardDrive, Zap } from "lucide-react";
import { PieChart, Pie, Cell, ResponsiveContainer, Tooltip } from "recharts";
import { StatCard, Card } from "@/components/Card";
import { useStats, useMemories, useProjects } from "@/api/hooks";
import { formatDate, truncate } from "@/lib/utils";

const COLORS = ["#7c5cfc", "#00d4aa", "#ffaa33", "#ff4466"];

export default function Dashboard() {
  const { data: stats, isLoading: statsLoading } = useStats();
  const { data: mems } = useMemories({ limit: 10 });
  const { data: projs } = useProjects();

  if (statsLoading || !stats) {
    return <div className="p-8 text-text-muted">Loading...</div>;
  }

  const cacheRate = stats.cache_hits + stats.cache_misses > 0
    ? Math.round((stats.cache_hits / (stats.cache_hits + stats.cache_misses)) * 100)
    : 0;

  const pieData = [
    { name: "Episodic", value: stats.total_episodic },
    { name: "Semantic", value: stats.total_semantic },
  ].filter((d) => d.value > 0);

  const wmPercent = stats.working_memory_capacity > 0
    ? Math.round((stats.working_memory_fill / stats.working_memory_capacity) * 100)
    : 0;

  return (
    <div className="p-6 space-y-6 max-w-7xl">
      <h1 className="text-2xl font-bold">Dashboard</h1>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard
          title="Total Memories"
          value={stats.total_episodic + stats.total_semantic}
          subtitle={`${stats.total_episodic} episodic · ${stats.total_semantic} semantic`}
          icon={<Database className="w-5 h-5" />}
        />
        <StatCard
          title="Working Memory"
          value={`${wmPercent}%`}
          subtitle={`${stats.working_memory_fill} / ${stats.working_memory_capacity}`}
          icon={<Cpu className="w-5 h-5" />}
        />
        <StatCard
          title="Cache Hit Rate"
          value={`${cacheRate}%`}
          subtitle={`${stats.cache_hits} hits · ${stats.cache_misses} misses`}
          icon={<Zap className="w-5 h-5" />}
        />
        <StatCard
          title="Embedding Model"
          value={stats.embedding_dims}
          subtitle={stats.embedding_model}
          icon={<HardDrive className="w-5 h-5" />}
        />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <Card className="lg:col-span-2">
          <h2 className="text-lg font-semibold mb-4">Recent Memories</h2>
          {mems?.memories?.length ? (
            <div className="space-y-2">
              {mems.memories.map((m) => (
                <div
                  key={m.id}
                  className="flex items-start gap-3 p-3 rounded-lg bg-surface-2 border border-border hover:border-border-hover transition-colors"
                >
                  <span className="shrink-0 mt-1 px-2 py-0.5 rounded text-[10px] font-mono uppercase bg-primary/15 text-primary">
                    {m.tier}
                  </span>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm leading-relaxed">{truncate(m.content, 150)}</p>
                    <div className="flex gap-3 mt-1 text-xs text-text-muted">
                      <span>importance: {m.importance.toFixed(2)}</span>
                      <span>{formatDate(m.created_at)}</span>
                      {m.tags?.map((t) => (
                        <span key={t} className="text-accent">#{t}</span>
                      ))}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-text-muted text-sm">No memories yet. Use Cursor to remember something!</p>
          )}
        </Card>

        <div className="space-y-4">
          <Card>
            <h2 className="text-lg font-semibold mb-3">Memory Distribution</h2>
            {pieData.length > 0 ? (
              <ResponsiveContainer width="100%" height={180}>
                <PieChart>
                  <Pie data={pieData} cx="50%" cy="50%" innerRadius={40} outerRadius={70} dataKey="value" paddingAngle={4}>
                    {pieData.map((_, i) => (
                      <Cell key={i} fill={COLORS[i % COLORS.length]} />
                    ))}
                  </Pie>
                  <Tooltip
                    contentStyle={{ background: "#1a1a28", border: "1px solid #2a2a3d", borderRadius: "8px" }}
                    labelStyle={{ color: "#e8e8f0" }}
                  />
                </PieChart>
              </ResponsiveContainer>
            ) : (
              <p className="text-text-muted text-sm text-center py-8">No data</p>
            )}
            <div className="flex justify-center gap-4 text-xs">
              {pieData.map((d, i) => (
                <span key={d.name} className="flex items-center gap-1">
                  <span className="w-2 h-2 rounded-full" style={{ background: COLORS[i] }} />
                  {d.name}: {d.value}
                </span>
              ))}
            </div>
          </Card>

          <Card>
            <h2 className="text-lg font-semibold mb-3">Projects</h2>
            {projs?.projects?.length ? (
              <div className="space-y-2">
                {projs.projects.map((p) => (
                  <div key={p.id} className="flex items-center gap-2 text-sm">
                    <span className="w-2 h-2 rounded-full bg-accent" />
                    <span className="font-medium">{p.display_name}</span>
                    <span className="text-text-muted text-xs">({p.slug})</span>
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-text-muted text-sm">No projects yet</p>
            )}
          </Card>
        </div>
      </div>
    </div>
  );
}
