import BacktestMenu from './BacktestMenu'

/** Both grid entry points share the persisted replay task API and result viewer. */
export default function OptimizerPage() {
  return <BacktestMenu optimizationOnly />
}
