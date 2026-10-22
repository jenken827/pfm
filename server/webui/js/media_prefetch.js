// 并行分片预取：把远端媒体按块用多条连接拉下来，攒成 Blob 再交给 <video>。
//
// 为什么需要它：链路（内网穿透 / 组网中转）实测持续只有 0.7~2.5MB/s 且会出现数秒的
// 断流窗口，浏览器直连流式播放时缓冲会被瞬时打空，表现成「播一下顿一下」，降码率也只能
// 缓解。把整份文件先拉到本地再播，播放阶段完全不碰网络，卡顿与链路抖动无关。
//
// 代价：必须等整份拉完才能起播（所以是用户显式点的按钮，不做默认路径）。

/**
 * 解析 `bytes 0-0/97996093` 里的总长度
 * @param {string|null} header
 * @return {number}
 */
export function parseContentRangeTotal(header) {
  if (!header) return 0;
  const slash = String(header).lastIndexOf('/');
  if (slash < 0) return 0;
  const total = Number(String(header).slice(slash + 1).trim());
  return Number.isFinite(total) && total > 0 ? total : 0;
}

/** 探测远端文件是否支持 Range，返回总字节数（不支持则抛错） */
async function probeSize(url, isAlive) {
  const response = await fetch(url, {
    headers: { Range: 'bytes=0-0' },
    cache: 'no-store',
  });
  const total = parseContentRangeTotal(response.headers.get('Content-Range'));
  // 释放探测响应体（1 字节）
  if (typeof response.arrayBuffer === 'function') {
    await response.arrayBuffer().catch(() => {});
  }
  if (response.status !== 206 || !total) {
    throw new Error(`服务器不支持分段下载（HTTP ${response.status}）`);
  }
  if (!isAlive()) throw new Error('已取消');
  return total;
}

/**
 * 并行拉取整份媒体，返回 Blob。
 *
 * @param {object} options
 * @param {string} options.url 带票据的媒体地址
 * @param {number} [options.connections] 并发连接数
 * @param {number} [options.chunkSize] 分块大小
 * @param {(progress: {percent: number, bytes: number, total: number, bytesPerSecond: number}) => void} [options.onProgress]
 * @param {() => boolean} [options.isAlive] 视图是否还在（翻页 / 关闭后放弃）
 */
export async function prefetchMedia({
  url,
  connections = 6,
  chunkSize = 1024 * 1024,
  onProgress,
  isAlive = () => true,
}) {
  const total = await probeSize(url, isAlive);
  const buffer = new Uint8Array(total);
  const chunkCount = Math.ceil(total / chunkSize);
  let nextIndex = 0;
  let doneChunks = 0;
  let doneBytes = 0;
  let failed = null;
  const startedAt = Date.now();

  const report = () => {
    if (!onProgress) return;
    const elapsed = Math.max(1, Date.now() - startedAt) / 1000;
    onProgress({
      percent: Math.min(100, Math.round((doneChunks / chunkCount) * 100)),
      bytes: doneBytes,
      total,
      bytesPerSecond: doneBytes / elapsed,
    });
  };

  const worker = async () => {
    for (;;) {
      if (failed || !isAlive()) return;
      const index = nextIndex;
      nextIndex += 1;
      if (index >= chunkCount) return;
      const start = index * chunkSize;
      const end = Math.min(total, start + chunkSize) - 1;
      try {
        const response = await fetch(url, {
          headers: { Range: `bytes=${start}-${end}` },
          cache: 'no-store',
        });
        if (response.status !== 206) {
          throw new Error(`分段请求失败（HTTP ${response.status}）`);
        }
        const data = new Uint8Array(await response.arrayBuffer());
        buffer.set(data, start);
        doneChunks += 1;
        doneBytes += data.length;
        report();
      } catch (error) {
        failed = error;
        return;
      }
    }
  };

  report();
  const workers = [];
  for (let i = 0; i < Math.min(connections, chunkCount); i += 1) {
    workers.push(worker());
  }
  await Promise.all(workers);
  if (failed) throw failed;
  if (!isAlive()) throw new Error('已取消');
  return new Blob([buffer], { type: 'video/mp4' });
}
