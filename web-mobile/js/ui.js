// Shared UI primitives used by both admin and viewer modules.

function StatusBadge({ status, showLiveDot }) {
  const map = {
    setup: ["badge--setup", "Setup"],
    pools: ["badge--pools", "Pools"],
    playoffs: ["badge--playoffs", "Playoffs"],
    completed: ["badge--completed", "Completed"],
  };
  const [cls, label] = map[status] || ["badge--setup", status];
  const showLive = showLiveDot && (status === "pools" || status === "playoffs");
  return (
    <span className={`badge ${cls}`}>
      {showLive && <span className="dot dot--live" style={{ marginRight: 4 }}></span>}
      {label}
    </span>
  );
}

function formatDate(d) {
  if (!d) return "Date TBA";
  const date = new Date(d + "T00:00");
  if (isNaN(date.getTime())) return "Date TBA";
  return date.toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" });
}

export { StatusBadge, formatDate };

if (typeof window !== "undefined") {
  window.StatusBadge = StatusBadge;
  window.formatDate = formatDate;
}
