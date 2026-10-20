# Product preview assets

These original WebP images capture the working TransactX representative UI
with intercepted synthetic test responses. They contain no private users,
production data or generated provider output and are labelled as fixtures on
the landing page. Each is a top-1,000px crop of its 1,440px-wide route capture,
encoded at WebP quality 88. Full-page before/after captures remain in the
existing private review directory, outside Git.

Source: `browser/visual-reset.fixture.spec.ts`; roles: customer overview,
merchant dashboard, operator payment journey with evidence-summary report.
Light/dark variants follow the selected theme. Images are imported through
Vite so their hashed static URLs enter the existing offline asset manifest.
These assets are product imagery, not live integration evidence or visual
acceptance. Refresh affected previews after an accepted layout changes.
