// What the share page works out from a snapshot, apart from the page itself: no DOM and no
// engine here, so the npm tests run it as it ships.

// The font the page serves (share.css), behind whatever family the snapshot names.
export const SERVED_FONT = 'CaskaydiaMono NF';

export const MISSING = 'This share has expired or never existed.';
export const NETWORK_FAILED = 'The share could not be loaded: the network request failed. Check the connection and reload the page.';

const ANSI = ['black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white',
  'brightBlack', 'brightRed', 'brightGreen', 'brightYellow', 'brightBlue', 'brightMagenta', 'brightCyan', 'brightWhite'];

// The id in a share page's path (/s/<id>), or null when the path holds none.
export function shareId(pathname) {
  const match = /^\/s\/([a-z2-7]{26})\/?$/.exec(pathname);
  return match ? match[1] : null;
}

// rioterm's Theme from the snapshot's colors; selection is never drawn (the view takes no
// input) but the Theme needs it.
export function rioTheme(theme) {
  const out = {
    foreground: theme.foreground,
    background: theme.background,
    cursor: theme.cursor,
    selectionForeground: theme.background,
    selectionBackground: theme.foreground,
  };
  ANSI.forEach((name, i) => { out[name] = theme.palette[i]; });
  return out;
}

// The CSS font stack: the snapshot's family (a viewer may have it installed), the served
// font, then any monospace font.
export function fontStack(family) {
  const families = [family, SERVED_FONT].filter((f, i, all) => f && all.indexOf(f) === i);
  return [...families.map(f => `"${f}"`), 'monospace'].join(', ');
}

// The font size, in CSS pixels, at which cols cells of advance em fill width.
export function fitFontSize(width, cols, advance) {
  if (!(width > 0) || !(cols > 0) || !(advance > 0)) return 14;
  return Math.min(64, Math.max(4, Math.floor(width / (cols * advance))));
}

const UNITS = [['year', 365 * 86400], ['month', 30 * 86400], ['week', 7 * 86400], ['day', 86400], ['hour', 3600], ['minute', 60]];

// "5 minutes ago", "yesterday", "just now".
export function relativeTime(then, now, locale) {
  const seconds = (then.getTime() - now.getTime()) / 1000;
  const format = new Intl.RelativeTimeFormat(locale, {numeric: 'auto'});
  for (const [unit, size] of UNITS) {
    if (Math.abs(seconds) >= size) return format.format(Math.round(seconds / size), unit);
  }
  return 'just now';
}

// The line under the title: "shared 5 minutes ago, expires Dec 18, 2026".
export function sharedLine(record, now, locale) {
  const created = new Date(record.created_at);
  const expires = new Date(record.expires_at);
  const date = new Intl.DateTimeFormat(locale, {dateStyle: 'medium'}).format(expires);
  return `shared ${relativeTime(created, now, locale)}, expires ${date}`;
}

// The sentence the page shows for a response that carried no snapshot.
export async function failureSentence(response) {
  let sentence = '';
  try { sentence = (await response.json()).error || ''; } catch { /* not JSON */ }
  if (response.status === 404) return sentence || MISSING;
  return `The share could not be loaded: the server answered HTTP ${response.status}. Reload the page to try again.`;
}

// The VT stream the engine is fed: the snapshot's, then the cursor hidden, since the view
// is read-only.
export function feed(record) {
  return record.vt + '\x1b[?25l';
}
