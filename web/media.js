import { state, orderedClips, $, seconds } from './state.js';
import { clipName } from './labels.js';

export function previewFrame(clip, time, p) {
  const visible = time >= clip.start_ms && time < clip.start_ms + clip.duration_ms;
  return {
    visible,
    source_ms: clip.source_in_ms + Math.max(0, Math.min(clip.duration_ms, time - clip.start_ms)),
    captions: p.clips.filter(
      (c) => c.text !== undefined && time >= c.start_ms && time < c.start_ms + c.duration_ms,
    ),
  };
}

export function currentMediaClip() {
  return (
    state.project?.clips.find((c) => c.id === state.selectedClipID && c.text === undefined) ||
    state.project?.clips.find((c) => c.id === state.mediaClipID && c.text === undefined) ||
    state.project?.tracks
      .flatMap((t) => orderedClips(state.project, t))
      .find((c) => c.text === undefined)
  );
}

export function updateMedia(seek = false) {
  const clip = currentMediaClip(),
    video = $('media-video'),
    image = $('media-image');
  if (!clip) {
    video.pause?.();
    video.hidden = image.hidden = true;
    $('media-empty').hidden = false;
    $('media-title').textContent = '先选择一个视频片段';
    return;
  }
  const changed = state.mediaClipID !== clip.id;
  state.mediaClipID = clip.id;
  if (seek || changed) state.mediaTime = clip.start_ms;
  const binding = state.mediaBindings.get(clip.id),
    frame = previewFrame(clip, state.mediaTime, state.project);
  $('media-title').textContent =
    `${clipName(clip)} · ${seconds(clip.start_ms)}—${seconds(clip.start_ms + clip.duration_ms)} · ${binding ? binding.name : '尚未绑定媒体'}`;
  $('media-cursor').max = String(
    Math.max(1000, ...state.project.clips.map((c) => c.start_ms + c.duration_ms)),
  );
  $('media-cursor').value = String(state.mediaTime);
  $('media-position').textContent = seconds(state.mediaTime);
  $('media-caption').textContent = frame.captions.map((c) => c.text).join('\n');
  video.hidden = !binding || binding.kind !== 'video' || !frame.visible;
  image.hidden = !binding || binding.kind !== 'image' || !frame.visible;
  $('media-empty').hidden = !!binding && frame.visible;
  $('media-empty').textContent = !frame.visible
    ? '当前时间不在所选片段内。拖动时间滑块可查看该片段。'
    : '为此片段绑定本地视频或图片；也可使用演示封面。';
  if (binding) {
    const element = binding.kind === 'video' ? video : image;
    if (element.dataset.binding !== binding.url) {
      video.pause?.();
      element.src = binding.url;
      element.dataset.binding = binding.url;
    }
    if (binding.kind === 'video') {
      if (!frame.visible) video.pause?.();
      else if (
        (seek || changed || video.paused) &&
        video.readyState >= 1 &&
        Math.abs(video.currentTime - frame.source_ms / 1000) > 0.05
      )
        video.currentTime = frame.source_ms / 1000;
    } else video.pause?.();
  } else video.pause?.();
}

export function clearMediaBindings() {
  $('media-video').pause?.();
  for (const url of new Set(
    [...state.mediaBindings.values()].filter((b) => b.local).map((b) => b.url),
  ))
    URL.revokeObjectURL(url);
  state.mediaBindings.clear();
  state.mediaClipID = null;
  state.mediaTime = 0;
  $('media-video').removeAttribute?.('src');
  $('media-image').removeAttribute?.('src');
  $('media-video').dataset.binding = '';
  $('media-image').dataset.binding = '';
}

export function bindMedia(file) {
  const clip = currentMediaClip();
  if (!clip) throw new Error('请先选择视频片段。');
  if (!/^video\//.test(file.type) && !/^image\/(png|jpeg|webp|gif)$/.test(file.type))
    throw new Error('请选择浏览器支持的视频或 PNG、JPEG、WebP、GIF 图片。');
  if (file.size > 512 * 1024 * 1024) throw new Error('预览文件不能超过 512 MiB。');
  state.mediaBindings.set(clip.id, {
    url: URL.createObjectURL(file),
    name: file.name,
    kind: file.type.startsWith('video/') ? 'video' : 'image',
    local: true,
  });
  updateMedia(true);
}
