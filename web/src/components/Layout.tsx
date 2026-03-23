import { NavLink, Outlet } from "react-router-dom";
import { Brain, Database, FolderOpen, Search, Clock, BarChart3, Settings } from "lucide-react";
import { cn } from "@/lib/utils";
import { useHealth } from "@/api/hooks";

const links = [
  { to: "/", icon: BarChart3, label: "Dashboard" },
  { to: "/memories", icon: Database, label: "Memories" },
  { to: "/projects", icon: FolderOpen, label: "Projects" },
  { to: "/recall", icon: Search, label: "Recall" },
  { to: "/timeline", icon: Clock, label: "Timeline" },
  { to: "/settings", icon: Settings, label: "Settings" },
];

export default function Layout() {
  const { data: health } = useHealth();
  const isUp = health?.status === "ok";

  return (
    <div className="flex h-screen">
      <aside className="w-56 shrink-0 border-r border-border bg-surface flex flex-col">
        <div className="p-4 flex items-center gap-2 border-b border-border">
          <Brain className="w-6 h-6 text-primary" />
          <span className="font-bold text-lg tracking-tight">Hippocampus</span>
        </div>

        <nav className="flex-1 p-2 space-y-0.5">
          {links.map(({ to, icon: Icon, label }) => (
            <NavLink
              key={to}
              to={to}
              end={to === "/"}
              className={({ isActive }) =>
                cn(
                  "flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors",
                  isActive
                    ? "bg-primary/15 text-primary font-medium"
                    : "text-text-muted hover:bg-surface-2 hover:text-text"
                )
              }
            >
              <Icon className="w-4 h-4" />
              {label}
            </NavLink>
          ))}
        </nav>

        <div className="p-3 border-t border-border text-xs text-text-muted flex items-center gap-2">
          <span className={cn("w-2 h-2 rounded-full", isUp ? "bg-accent" : "bg-danger")} />
          {isUp ? `v${health.version}` : "offline"}
        </div>
      </aside>

      <main className="flex-1 overflow-auto">
        <Outlet />
      </main>
    </div>
  );
}
