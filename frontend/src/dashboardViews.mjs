export const dashboardViews = [
  {
    key: 'overview',
    label: 'Overview',
    title: 'Monitoring status',
    description: 'A read-only view of systemd workers and RabbitMQ queues.',
    eyebrow: 'Monitored sources',
    emptyTitle: 'No sources configured',
    emptyDescription: 'Nothing is being monitored yet. Source configuration is not part of this starter setup.',
    emptyDetails: 'Once sources are configured, systemd service states and RabbitMQ queue observations can appear here.',
  },
  {
    key: 'services',
    label: 'Systemd workers',
    title: 'Systemd workers',
    description: 'Current state of configured systemd worker services.',
    eyebrow: 'Worker services',
    emptyTitle: 'No systemd workers configured',
    emptyDescription: 'No worker services are being monitored yet.',
    emptyDetails: 'Once a host and worker units are configured, service states and observation times will appear here.',
  },
  {
    key: 'queues',
    label: 'RabbitMQ queues',
    title: 'RabbitMQ queues',
    description: 'Current state of configured queues and their consumers.',
    eyebrow: 'Queue monitoring',
    emptyTitle: 'No RabbitMQ queues configured',
    emptyDescription: 'No queues are being monitored yet.',
    emptyDetails: 'Once a source and queues are configured, message counts, consumers, and observation times will appear here.',
  },
]

export function dashboardViewByKey(key) {
  return dashboardViews.find((view) => view.key === key) ?? dashboardViews[0]
}
