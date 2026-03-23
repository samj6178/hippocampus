import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";

export function useStats() {
  return useQuery({ queryKey: ["stats"], queryFn: api.getStats, refetchInterval: 5000 });
}

export function useHealth() {
  return useQuery({ queryKey: ["health"], queryFn: api.getHealth, refetchInterval: 10000 });
}

export function useMemories(params?: { project?: string; limit?: number; offset?: number }) {
  return useQuery({
    queryKey: ["memories", params],
    queryFn: () => api.listMemories(params),
  });
}

export function useMemory(id: string) {
  return useQuery({
    queryKey: ["memory", id],
    queryFn: () => api.getMemory(id),
    enabled: !!id,
  });
}

export function useCreateMemory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.createMemory,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["memories"] });
      qc.invalidateQueries({ queryKey: ["stats"] });
    },
  });
}

export function useDeleteMemory() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.deleteMemory,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["memories"] });
      qc.invalidateQueries({ queryKey: ["stats"] });
    },
  });
}

export function useRecall() {
  return useMutation({ mutationFn: api.recall });
}

export function useProjects() {
  return useQuery({ queryKey: ["projects"], queryFn: api.listProjects });
}

export function useProjectStats(slug: string) {
  return useQuery({
    queryKey: ["project-stats", slug],
    queryFn: () => api.getProjectStats(slug),
    enabled: !!slug,
  });
}

export function useCreateProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.createProject,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["projects"] }),
  });
}

export function useDeleteProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.deleteProject,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["projects"] }),
  });
}
