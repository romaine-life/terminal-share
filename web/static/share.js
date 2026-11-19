// The share page: fetch the snapshot, then draw it read-only with rioterm (Rio's VT engine,
// compiled to WebAssembly) at a size that fills the page's width.
import {MISSING, NETWORK_FAILED, SERVED_FONT, failureSentence, feed, fitFontSize, fontStack, rioTheme, shareId, sharedLine} from './share-lib.js';

const status = document.getElementById('status');
const titleEl = document.getElementById('title');
const metaEl = document.getElementById('meta');
const host = document.getElementById('terminal');

function show(sentence, problem = true) {
  status.textContent = sentence;
  status.classList.toggle('problem', problem);
  status.hidden = false;
  host.hidden = true;
}

async function loadRecord(id) {
  let response;
  try {
    response = await fetch(`/api/snapshots/${id}`, {headers: {Accept: 'application/json'}});
  } catch {
    return {sentence: NETWORK_FAILED};
  }
  if (!response.ok) return {sentence: await failureSentence(response)};
  try {
    return {record: await response.json()};
  } catch {
    return {sentence: NETWORK_FAILED};
  }
}

async function loadEngine() {
  const rio = await import('./vendor/rioterm/dist/index.js');
  await rio.initWasm();
  return rio;
}

// The served font has to be loaded before the renderer measures its cells.
async function loadFonts() {
  try {
    await Promise.all([document.fonts.load(`16px "${SERVED_FONT}"`), document.fonts.load(`bold 16px "${SERVED_FONT}"`)]);
  } catch {
    // The terminal draws in a fallback monospace font.
  }
}

// The advance of one cell in em, as rioterm measures it ("W").
function advanceOf(stack) {
  const ctx = document.createElement('canvas').getContext('2d');
  ctx.font = `100px ${stack}`;
  return ctx.measureText('W').width / 100;
}

function draw(rio, record) {
  const term = new rio.Terminal({cols: record.cols, rows: record.rows, scrollback: 0, detectUrls: false});
  term.write(feed(record));
  const stack = fontStack(record.font.family);
  const advance = advanceOf(stack);
  const theme = rioTheme(record.theme);
  host.style.background = record.theme.background;
  let renderer = null;
  let size = 0;
  const fit = () => {
    const width = host.clientWidth - 16; // #terminal's padding
    const next = fitFontSize(width, record.cols, advance);
    if (next === size) return;
    size = next;
    renderer?.dispose();
    renderer = new rio.CanvasRenderer(term, {fontFamily: stack, fontSize: size, lineHeight: 1.2, theme, cursorStyle: 'block'});
    // The renderer sizes its canvas in pixels; the page scales it to the width instead.
    renderer.canvas.style.width = '100%';
    renderer.canvas.style.height = 'auto';
    host.appendChild(renderer.canvas);
  };
  status.hidden = true;
  host.hidden = false;
  fit();
  let pending = 0;
  window.addEventListener('resize', () => {
    if (pending) return;
    pending = requestAnimationFrame(() => { pending = 0; fit(); });
  });
}

async function main() {
  const id = shareId(location.pathname);
  if (!id) {
    show(MISSING);
    return;
  }
  const engine = loadEngine();
  engine.catch(() => {}); // Reported below, once the snapshot has loaded.
  const {record, sentence} = await loadRecord(id);
  if (!record) {
    show(sentence);
    return;
  }
  const title = record.title || 'Untitled window';
  document.title = `${title} · terminal-share`;
  titleEl.textContent = title;
  metaEl.textContent = sharedLine(record, new Date());
  let rio;
  try {
    [rio] = await Promise.all([engine, loadFonts()]);
  } catch (error) {
    show(`The terminal engine could not be loaded (${error?.message || error}). Reload the page to try again.`);
    return;
  }
  try {
    draw(rio, record);
  } catch (error) {
    show(`The terminal could not be drawn (${error?.message || error}).`);
  }
}

main();
