import { useState, useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import { Card } from "@/components/Card";
import { CheckCircle2, XCircle, Loader2, Zap } from "lucide-react";

const PRESETS: Record<string, { base_url: string; model: string }> = {
  ollama: { base_url: "http://localhost:11434/v1", model: "qwen2.5:7b" },
  deepseek: { base_url: "https://api.deepseek.com/v1", model: "deepseek-chat" },
  qwen: { base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1", model: "qwen-plus" },
  openai: { base_url: "https://api.openai.com/v1", model: "gpt-4o-mini" },
  openrouter: { base_url: "https://openrouter.ai/api/v1", model: "deepseek/deepseek-chat" },
};

export default function Settings() {
  const qc = useQueryClient();
  const { data: status, isLoading } = useQuery({
    queryKey: ["llm-status"],
    queryFn: api.getLLMSettings,
    refetchInterval: 15000,
  });

  const [provider, setProvider] = useState("ollama");
  const [baseUrl, setBaseUrl] = useState("http://localhost:11434/v1");
  const [apiKey, setApiKey] = useState("");
  const [model, setModel] = useState("qwen2.5:7b");
  const [maxRpm, setMaxRpm] = useState(60);

  useEffect(() => {
    if (status) {
      setBaseUrl(status.base_url || "http://localhost:11434/v1");
      setModel(status.model || "qwen2.5:7b");
      const detected = Object.entries(PRESETS).find(
        ([, p]) => status.base_url?.includes(new URL(p.base_url).hostname)
      );
      if (detected) setProvider(detected[0]);
      else setProvider("custom");
    }
  }, [status]);

  const saveMutation = useMutation({
    mutationFn: api.updateLLMSettings,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["llm-status"] }),
  });

  const testMutation = useMutation({
    mutationFn: api.testLLMConnection,
  });

  const handlePresetChange = (preset: string) => {
    setProvider(preset);
    if (preset in PRESETS) {
      setBaseUrl(PRESETS[preset].base_url);
      setModel(PRESETS[preset].model);
    }
  };

  const handleSave = () => {
    saveMutation.mutate({ provider, base_url: baseUrl, api_key: apiKey, model, max_rpm: maxRpm });
  };

  const handleTest = () => {
    testMutation.mutate({ base_url: baseUrl, api_key: apiKey, model, max_rpm: maxRpm });
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="w-8 h-8 animate-spin text-primary" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-2xl mx-auto space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Settings</h1>
        <p className="text-text-muted text-sm mt-1">
          Configure the LLM provider for research agents, synthesis, and reranking.
        </p>
      </div>

      <Card>
        <div className="p-5 space-y-5">
          <div className="flex items-center justify-between">
            <h2 className="text-lg font-semibold">LLM Provider</h2>
            <StatusBadge available={status?.available} />
          </div>

          <div>
            <label className="block text-sm font-medium mb-2">Provider</label>
            <div className="grid grid-cols-3 gap-2">
              {["ollama", "deepseek", "qwen", "openai", "openrouter", "custom"].map((p) => (
                <button
                  key={p}
                  onClick={() => handlePresetChange(p)}
                  className={`px-3 py-2 rounded-lg text-sm font-medium border transition-colors ${
                    provider === p
                      ? "border-primary bg-primary/10 text-primary"
                      : "border-border bg-surface hover:bg-surface-2 text-text-muted"
                  }`}
                >
                  {p === "ollama" ? "Ollama (local)" : p.charAt(0).toUpperCase() + p.slice(1)}
                </button>
              ))}
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium mb-1">Base URL</label>
            <input
              type="text"
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
              className="w-full px-3 py-2 rounded-lg border border-border bg-surface text-sm focus:outline-none focus:ring-2 focus:ring-primary/50"
              placeholder="https://api.deepseek.com/v1"
            />
          </div>

          <div>
            <label className="block text-sm font-medium mb-1">API Key</label>
            <input
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              className="w-full px-3 py-2 rounded-lg border border-border bg-surface text-sm focus:outline-none focus:ring-2 focus:ring-primary/50"
              placeholder={provider === "ollama" ? "Not required for Ollama" : "sk-..."}
            />
            {provider === "ollama" && (
              <p className="text-xs text-text-muted mt-1">Ollama doesn't require an API key.</p>
            )}
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-1">Model</label>
              <input
                type="text"
                value={model}
                onChange={(e) => setModel(e.target.value)}
                className="w-full px-3 py-2 rounded-lg border border-border bg-surface text-sm focus:outline-none focus:ring-2 focus:ring-primary/50"
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">Max RPM</label>
              <input
                type="number"
                value={maxRpm}
                onChange={(e) => setMaxRpm(Number(e.target.value))}
                className="w-full px-3 py-2 rounded-lg border border-border bg-surface text-sm focus:outline-none focus:ring-2 focus:ring-primary/50"
                min={1}
                max={1000}
              />
            </div>
          </div>

          <div className="flex items-center gap-3 pt-2">
            <button
              onClick={handleTest}
              disabled={testMutation.isPending}
              className="px-4 py-2 rounded-lg border border-border text-sm font-medium hover:bg-surface-2 transition-colors disabled:opacity-50 flex items-center gap-2"
            >
              {testMutation.isPending ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <Zap className="w-4 h-4" />
              )}
              Test Connection
            </button>

            <button
              onClick={handleSave}
              disabled={saveMutation.isPending}
              className="px-4 py-2 rounded-lg bg-primary text-white text-sm font-medium hover:bg-primary/90 transition-colors disabled:opacity-50 flex items-center gap-2"
            >
              {saveMutation.isPending && <Loader2 className="w-4 h-4 animate-spin" />}
              Save
            </button>

            {testMutation.isSuccess && (
              <span className="flex items-center gap-1 text-sm">
                {testMutation.data.available ? (
                  <>
                    <CheckCircle2 className="w-4 h-4 text-accent" />
                    <span className="text-accent">Connected</span>
                  </>
                ) : (
                  <>
                    <XCircle className="w-4 h-4 text-danger" />
                    <span className="text-danger">Failed</span>
                  </>
                )}
              </span>
            )}

            {saveMutation.isSuccess && (
              <span className="text-sm text-accent">Saved!</span>
            )}

            {saveMutation.isError && (
              <span className="text-sm text-danger">
                {(saveMutation.error as Error).message}
              </span>
            )}
          </div>
        </div>
      </Card>

      {status && (
        <Card>
          <div className="p-5 space-y-2">
            <h2 className="text-lg font-semibold">Current Status</h2>
            <div className="text-sm space-y-1 text-text-muted">
              <p>
                <span className="font-medium text-text">Provider:</span> {status.provider_name}
              </p>
              <p>
                <span className="font-medium text-text">Base URL:</span> {status.base_url}
              </p>
              <p>
                <span className="font-medium text-text">Model:</span> {status.model}
              </p>
              <p>
                <span className="font-medium text-text">API Key:</span>{" "}
                {status.api_key_set ? "Set" : "Not set"}
              </p>
            </div>
          </div>
        </Card>
      )}
    </div>
  );
}

function StatusBadge({ available }: { available?: boolean }) {
  if (available === undefined) return null;
  return available ? (
    <span className="flex items-center gap-1 text-xs font-medium text-accent bg-accent/10 px-2 py-1 rounded-full">
      <span className="w-1.5 h-1.5 rounded-full bg-accent" />
      Connected
    </span>
  ) : (
    <span className="flex items-center gap-1 text-xs font-medium text-danger bg-danger/10 px-2 py-1 rounded-full">
      <span className="w-1.5 h-1.5 rounded-full bg-danger" />
      Offline
    </span>
  );
}
