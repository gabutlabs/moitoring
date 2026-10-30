export interface DashboardView {
  key: 'overview' | 'services' | 'queues'
  label: string
  title: string
  description: string
  eyebrow: string
  emptyTitle: string
  emptyDescription: string
  emptyDetails: string
}

export const dashboardViews: DashboardView[]

export function dashboardViewByKey(key: string): DashboardView
