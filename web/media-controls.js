import { $, state } from './state.js';
import { bindMedia, currentMediaClip, updateMedia, previewFrame } from './media.js';
import { toast } from './ui.js';

export function installMediaControls() {
  $('media-file').addEventListener('change', () => {
    const file = $('media-file').files?.[0];
    if (!file) return;
    try {
      bindMedia(file);
    } catch (err) {
      toast(err.message);
    } finally {
      $('media-file').value = '';
    }
  });

  $('demo-cover').addEventListener('click', () => {
    const clip = currentMediaClip();
    if (!clip) return;
    state.mediaBindings.set(clip.id, {
      url: '/demo-cover.svg',
      name: '虚构演示封面',
      kind: 'image',
      local: false,
    });
    updateMedia(true);
  });

  $('media-cursor').addEventListener('input', () => {
    state.mediaTime = Number($('media-cursor').value);
    $('media-video').pause?.();
    updateMedia(false);
    const clip = currentMediaClip();
    if (clip) {
      const frame = previewFrame(clip, state.mediaTime, state.project);
      if (frame.visible && $('media-video').readyState >= 1)
        $('media-video').currentTime = frame.source_ms / 1000;
    }
  });

  $('media-video').addEventListener('loadedmetadata', () => {
    const clip = currentMediaClip();
    if (!clip) return;
    const v = $('media-video');
    v.currentTime = clip.source_in_ms / 1000;
    if (v.duration * 1000 < clip.source_in_ms + clip.duration_ms)
      $('media-note').textContent =
        '绑定视频短于工程声明的素材区间，超出部分无法播放；工程数据没有改变。';
  });

  $('media-video').addEventListener('timeupdate', () => {
    const clip = currentMediaClip(),
      v = $('media-video');
    if (!clip || v.hidden) return;
    state.mediaTime = clip.start_ms + Math.round(v.currentTime * 1000) - clip.source_in_ms;
    if (state.mediaTime >= clip.start_ms + clip.duration_ms) {
      v.pause();
      state.mediaTime = clip.start_ms + clip.duration_ms;
    } else if (state.mediaTime < clip.start_ms) {
      v.currentTime = clip.source_in_ms / 1000;
      state.mediaTime = clip.start_ms;
    }
    updateMedia();
  });

  $('media-video').addEventListener('error', () => {
    if (state.mediaBindings.get(state.mediaClipID)?.kind === 'video')
      toast('此视频无法预览，请换用浏览器支持的视频格式；工程没有改变。');
  });

  $('media-image').addEventListener('error', () =>
    toast('图片无法预览，请重新绑定；工程没有改变。'),
  );
}
