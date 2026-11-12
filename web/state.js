export const state = {
  maxTextChars: 2000,
  project: null,
  busy: false,
  dirty: false,
  modelAvailable: false,
  selectedClipID: null,
  selectedProjectID: null,
  generationController: null,
  auditState: { entries: [], can_undo: false, can_redo: false },
  activeDifference: null,
  mediaBindings: new Map(),
  mediaTime: 0,
  mediaClipID: null,
  pendingCards: new Set(),
};

export const textLength = (text) => Array.from(text).length;

export const $ = (id) => document.getElementById(id);

export const make = (tag, className, text) => {
  const e = document.createElement(tag);
  if (className) e.className = className;
  if (text !== undefined) e.textContent = text;
  return e;
};

export const seconds = (ms) => {
  const whole = Math.floor(ms / 1000),
    fraction = String(ms % 1000)
      .padStart(3, '0')
      .replace(/0+$/, '');
  return `${Math.floor(whole / 60)}:${String(whole % 60).padStart(2, '0')}${fraction ? '.' + fraction : ''}`;
};

export const durationText = (ms) => `${Number((ms / 1000).toFixed(3))} 秒`;

export const orderedClips = (p, track) =>
  p.clips
    .filter((c) => c.track_id === track.id)
    .sort((a, b) => a.start_ms - b.start_ms || a.id.localeCompare(b.id));
