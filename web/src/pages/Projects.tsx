import { useState } from "react";
import { Trash2, Plus, FolderOpen } from "lucide-react";
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer } from "recharts";
import { Card } from "@/components/Card";
import { useProjects, useCreateProject, useDeleteProject, useProjectStats } from "@/api/hooks";
import { formatDate } from "@/lib/utils";

function ProjectStatsChart({ slug }: { slug: string }) {
  const { data } = useProjectStats(slug);
  if (!data?.by_tier) return null;

  const chartData = Object.entries(data.by_tier).map(([tier, count]) => ({ tier, count }));
  if (chartData.length === 0) return <p className="text-text-muted text-xs">No data</p>;

  return (
    <ResponsiveContainer width="100%" height={120}>
      <BarChart data={chartData}>
        <XAxis dataKey="tier" tick={{ fill: "#8888a8", fontSize: 10 }} axisLine={false} tickLine={false} />
        <YAxis hide />
        <Tooltip
          contentStyle={{ background: "#1a1a28", border: "1px solid #2a2a3d", borderRadius: "8px", fontSize: 12 }}
        />
        <Bar dataKey="count" fill="#7c5cfc" radius={[4, 4, 0, 0]} />
      </BarChart>
    </ResponsiveContainer>
  );
}

export default function Projects() {
  const { data, isLoading } = useProjects();
  const createMut = useCreateProject();
  const deleteMut = useDeleteProject();
  const [showForm, setShowForm] = useState(false);
  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");

  const handleCreate = async () => {
    if (!slug.trim()) return;
    await createMut.mutateAsync({
      slug,
      display_name: name || slug,
      description: desc,
    });
    setSlug("");
    setName("");
    setDesc("");
    setShowForm(false);
  };

  const handleDelete = async (s: string) => {
    if (!confirm(`Delete project "${s}"?`)) return;
    await deleteMut.mutateAsync(s);
  };

  return (
    <div className="p-6 max-w-7xl space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Projects</h1>
        <button
          onClick={() => setShowForm(!showForm)}
          className="flex items-center gap-1 px-3 py-2 rounded-lg bg-primary text-white text-sm hover:bg-primary-hover transition-colors"
        >
          <Plus className="w-4 h-4" /> New Project
        </button>
      </div>

      {showForm && (
        <Card>
          <div className="space-y-3">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <input
                value={slug}
                onChange={(e) => setSlug(e.target.value)}
                placeholder="Slug (e.g. energy-monitor)"
                className="bg-surface-2 border border-border rounded-lg px-3 py-2 text-sm text-text focus:outline-none focus:border-primary"
              />
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Display name"
                className="bg-surface-2 border border-border rounded-lg px-3 py-2 text-sm text-text focus:outline-none focus:border-primary"
              />
            </div>
            <textarea
              value={desc}
              onChange={(e) => setDesc(e.target.value)}
              placeholder="Description..."
              rows={2}
              className="w-full bg-surface-2 border border-border rounded-lg p-3 text-sm text-text resize-none focus:outline-none focus:border-primary"
            />
            <button
              onClick={handleCreate}
              disabled={createMut.isPending}
              className="px-4 py-2 rounded-lg bg-accent text-bg text-sm font-medium hover:opacity-90 disabled:opacity-50"
            >
              {createMut.isPending ? "Creating..." : "Create"}
            </button>
          </div>
        </Card>
      )}

      {isLoading ? (
        <p className="text-text-muted">Loading...</p>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {data?.projects?.map((p) => (
            <Card key={p.id}>
              <div className="flex items-start justify-between mb-3">
                <div className="flex items-center gap-2">
                  <FolderOpen className="w-5 h-5 text-primary" />
                  <div>
                    <h3 className="font-semibold">{p.display_name}</h3>
                    <span className="text-xs text-text-muted font-mono">{p.slug}</span>
                  </div>
                </div>
                <button
                  onClick={() => handleDelete(p.slug)}
                  className="p-1.5 rounded-lg text-text-muted hover:text-danger hover:bg-danger/10 transition-colors"
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
              {p.description && <p className="text-sm text-text-muted mb-3">{p.description}</p>}
              <ProjectStatsChart slug={p.slug} />
              <div className="text-xs text-text-muted mt-2">Created {formatDate(p.created_at)}</div>
            </Card>
          ))}

          {!data?.projects?.length && (
            <p className="text-text-muted text-sm col-span-full">No projects yet. Create one to start organizing memories.</p>
          )}
        </div>
      )}
    </div>
  );
}
