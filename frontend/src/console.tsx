/**
 * console.tsx
 *
 * Operations Console facade re-exporting the modular operations architecture
 * under src/features/operations/. Preserves complete backwards compatibility for
 * main.tsx, console.test.ts, and existing fixture specs.
 */

export {
  OperationsShell as ConsoleShell,
  readConsoleView,
  consoleViewPath,
  permittedConsoleView,
  buildChaosPayload,
  SUPPORTED_CHAOS_TYPES,
  type ChaosFaultType,
  deriveOperationalTargets,
  RoutingView as ConsoleRoutingView,
  ReconciliationView as ConsoleReconciliationView,
  MerkleView as ConsoleMerkleView,
  IntegrityView as ConsoleIntegrityView,
  ChaosView as ConsoleChaosView,
  ActivityView as ConsoleActivityView,
  ConsoleIcon,
  formatIso,
} from "./features/operations";
