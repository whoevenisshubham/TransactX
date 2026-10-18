import { BrandMark, Skeleton } from "../components";

export function LoadingShell() {
  return (
    <div className="product-shell">
      <aside className="sidebar">
        <div className="brand-lockup">
          <BrandMark />
          <span>TransactX</span>
          <span className="customer-tag">Personal</span>
        </div>
      </aside>
      <main className="main-content">
        <div className="content-wrap">
          <Skeleton className="skeleton-heading" />
          <div className="skeleton-block" />
          <div className="skeleton-block skeleton-short" />
        </div>
      </main>
    </div>
  );
}

export function DetailsSkeleton() {
  return (
    <div className="details-page">
      <Skeleton className="skeleton-heading" />
      <div className="skeleton-block" />
      <div className="skeleton-block skeleton-short" />
    </div>
  );
}

