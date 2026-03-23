const BASE = "/api/v1";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { "Content-Type": "application/json", ...init?.headers },
    ...init,
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || `HTTP ${res.status}`);
  }
  return res.json();
}

export interface Memory {
  id: string;
  project_id?: string;
  tier: string;
  content: string;
  importance: number;
  token_count: number;
  tags?: string[];
  agent_id: string;
  created_at: string;
}

export interface EpisodicMemory extends Memory {
  summary?: string;
  confidence: number;
  access_count: number;
  session_id: string;
  embedding?: number[];
  last_accessed: string;
  updated_at: string;
}

export interface Project {
  id: string;
  slug: string;
  display_name: string;
  description: string;
  is_active: boolean;
  created_at: string;
}

export interface ProjectStats {
  project_id: string;
  slug: string;
  by_tier: Record<string, number>;
  last_active: string;
}

export interface SystemStats {
  total_episodic: number;
  total_semantic: number;
  working_memory_fill: number;
  working_memory_capacity: number;
  cache_hits: number;
  cache_misses: number;
  cache_size: number;
  embedding_model: string;
  embedding_dims: number;
}

export interface RecallResponse {
  context: {
    text: string;
    sources: { memory_id: string; tier: string; relevance: number; snippet: string }[];
    token_count: number;
    confidence: number;
  };
  candidates_considered: number;
}

export interface EncodeResponse {
  memory_id: string;
  encoded: boolean;
  gate_score: number;
  token_count: number;
}

export const api = {
  getHealth: () => request<{ status: string; version: string }>("/health"),
  getStats: () => request<SystemStats>("/stats"),

  listMemories: (params?: { project?: string; limit?: number; offset?: number }) => {
    const q = new URLSearchParams();
    if (params?.project) q.set("project", params.project);
    if (params?.limit) q.set("limit", String(params.limit));
    if (params?.offset) q.set("offset", String(params.offset));
    return request<{ memories: Memory[]; total: number }>(`/memories?${q}`);
  },

  getMemory: (id: string) => request<EpisodicMemory>(`/memories/${id}`),

  createMemory: (data: { content: string; project?: string; importance?: number; tags?: string[] }) =>
    request<EncodeResponse>("/memories", { method: "POST", body: JSON.stringify(data) }),

  deleteMemory: (id: string) =>
    request<{ status: string }>(`/memories/${id}`, { method: "DELETE" }),

  recall: (data: { query: string; project?: string; budget_tokens?: number }) =>
    request<RecallResponse>("/memories/recall", { method: "POST", body: JSON.stringify(data) }),

  listProjects: () => request<{ projects: Project[] }>("/projects"),

  createProject: (data: { slug: string; display_name: string; description?: string }) =>
    request<Project>("/projects", { method: "POST", body: JSON.stringify(data) }),

  getProject: (slug: string) => request<Project>(`/projects/${slug}`),

  getProjectStats: (slug: string) => request<ProjectStats>(`/projects/${slug}/stats`),

  deleteProject: (slug: string) =>
    request<{ status: string }>(`/projects/${slug}`, { method: "DELETE" }),

  getLLMSettings: () =>
    request<{
      provider_name: string;
      base_url: string;
      model: string;
      api_key_set: boolean;
      available: boolean;
    }>("/settings/llm"),

  updateLLMSettings: (data: {
    provider?: string;
    base_url: string;
    api_key: string;
    model: string;
    max_rpm: number;
  }) =>
    request<{
      provider_name: string;
      base_url: string;
      model: string;
      api_key_set: boolean;
      available: boolean;
    }>("/settings/llm", { method: "PUT", body: JSON.stringify(data) }),

  testLLMConnection: (data: {
    base_url: string;
    api_key?: string;
    model?: string;
    max_rpm?: number;
  }) =>
    request<{ available: boolean; provider: string }>("/settings/llm/test", {
      method: "POST",
      body: JSON.stringify(data),
    }),
};
