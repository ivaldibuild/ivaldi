const day = new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'short', year: 'numeric' });
const dayAndTime = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
});

/** A date as the web app shows it, such as 6 Nov 2026. */
export function formatDay(iso: string): string {
  return day.format(new Date(iso));
}

/** A date and time as the web app shows it, such as 6 Nov 2026, 14:05. */
export function formatDayAndTime(iso: string): string {
  return dayAndTime.format(new Date(iso));
}
