// 主题引导。CSP 禁止内联脚本，所以单独放一个 classic script 在 <head> 里同步执行：
// 首屏渲染前就把 <html data-theme> 写好，深色用户不会先看到一眼白屏。逻辑在 theme.js。
(function () {
  try {
    var mode = localStorage.getItem('pfm_web_theme');
    if (mode === 'light' || mode === 'dark') {
      document.documentElement.setAttribute('data-theme', mode);
    }
  } catch (e) {
    /* 隐私模式下 localStorage 可能抛错，忽略：跟着系统走即可 */
  }
})();
