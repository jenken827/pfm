// 在装载 js/ 下的模块之前，先把最小的浏览器全局放到 globalThis 上。
//
// 原因：`js/store.js` 在**模块求值**时就读 localStorage（记住列表 / 网格偏好），
// 而 ESM 的 import 会先于任何语句执行；测试文件里"先 install() 再 import"没用。
// 这个文件在 harness.mjs 里第一个 import，顺序有保证。

const storage = new Map();

if (typeof globalThis.localStorage === 'undefined') {
  Object.defineProperty(globalThis, 'localStorage', {
    value: {
      getItem: (k) => (storage.has(k) ? storage.get(k) : null),
      setItem: (k, v) => storage.set(k, String(v)),
      removeItem: (k) => storage.delete(k),
      clear: () => storage.clear(),
      key: (i) => [...storage.keys()][i] ?? null,
      get length() { return storage.size; },
    },
    configurable: true,
    writable: true,
  });
}

if (typeof globalThis.window === 'undefined') {
  globalThis.window = globalThis;
}

export {};
