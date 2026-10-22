// files.bound_provider_ids 是 JSON 数组字符串（如 "[1,2]"），解析成数字数组。
// 容错：空串 / 非法 JSON / 非数组一律当"未绑定"。

export function parseProviderIds(raw) {
  if (!raw) return [];
  try {
    const value = JSON.parse(raw);
    if (!Array.isArray(value)) return [];
    return value.map((v) => Number(v)).filter((v) => Number.isFinite(v));
  } catch {
    return [];
  }
}

export function formatProviderIds(ids) {
  return JSON.stringify(Array.isArray(ids) ? ids.map(Number) : []);
}
