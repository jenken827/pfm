// 缩略图加载器。
//
// 缩略图接口要 Authorization 头，`<img src>` 带不了，所以走 fetch + Blob →
// objectURL。一次列表可能有上百张图，这里用并发池限流，并在离开页面时统一
// revoke（不 revoke 会一直占着内存）。

import { fetchBlob, previewPath } from './api.js';

const DEFAULT_CONCURRENCY = 6;

export class ThumbLoader {
  constructor(concurrency = DEFAULT_CONCURRENCY) {
    this.concurrency = concurrency;
    this.active = 0;
    this.queue = [];
    this.urls = new Set();
    this.cancelled = false;
  }

  /**
   * 加载缩略图；成功后回调 objectURL（失败静默——列表里留图标即可，不弹错误）。
   * @returns {Promise<void>}
   */
  load(fileId, onUrl, size = '') {
    if (this.cancelled) return Promise.resolve();
    return new Promise((resolve) => {
      this.queue.push({ fileId, onUrl, size, resolve });
      this.pump();
    });
  }

  pump() {
    while (this.active < this.concurrency && this.queue.length) {
      const job = this.queue.shift();
      this.active += 1;
      this.run(job).finally(() => {
        this.active -= 1;
        job.resolve();
        this.pump();
      });
    }
  }

  async run(job) {
    if (this.cancelled) return;
    try {
      const blob = await fetchBlob(previewPath(job.fileId, { size: job.size }));
      if (this.cancelled) return;
      const url = URL.createObjectURL(blob);
      this.urls.add(url);
      job.onUrl(url);
    } catch {
      // 缩略图失败不影响列表可用性
    }
  }

  /** 重新渲染列表时调用：释放上一批 objectURL，但保持加载器可用 */
  reset() {
    this.queue = [];
    this.urls.forEach((url) => URL.revokeObjectURL(url));
    this.urls.clear();
  }

  dispose() {
    this.reset();
    this.cancelled = true;
  }
}
