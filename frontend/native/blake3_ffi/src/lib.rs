//! BLAKE3 的 C ABI，供 Flutter 客户端经 `dart:ffi` 调用。
//!
//! 接口故意做到最小：create / update / final / free。
//! 约定（两侧必须同时遵守）：
//!
//! * 句柄只能在同一线程内使用（Dart 侧在 worker isolate 里串行调用）；
//! * `final` **不**释放句柄，调用方必须自己 `free`（否则 worker isolate 反复哈希会泄漏）；
//! * 任何入口都不许 panic 穿过 FFI 边界（Android 上会直接 abort 整个进程），
//!   内部用 `catch_unwind` 兜住并返回负数错误码；
//! * 摘要固定 32 字节，与后端 `blake3.New(32, nil)` 一致。
//!
//! 错误码：0 成功，-1 参数非法（空指针 / 输出缓冲不足），-2 内部 panic。

use blake3::Hasher;
use std::panic::{catch_unwind, AssertUnwindSafe};
use std::slice;

/// ABI 版本。Dart 侧加载后会先取值校验，不匹配就退回纯 Dart 实现，
/// 避免「旧 .so 配新 ABI」算出错误摘要这种最难查的故障。
pub const ABI_VERSION: u32 = 1;

/// 摘要长度（字节）。
pub const DIGEST_LEN: usize = 32;

/// 超过这个长度才交给 rayon 并行：小块并行反而被调度开销吃掉。
const PARALLEL_MIN_LEN: usize = 1 << 20;

#[no_mangle]
pub extern "C" fn pfm_blake3_abi_version() -> u32 {
    ABI_VERSION
}

/// 新建一个哈希句柄。失败（内存不足）时返回空指针。
#[no_mangle]
pub extern "C" fn pfm_blake3_new() -> *mut Hasher {
    Box::into_raw(Box::new(Hasher::new()))
}

/// 追加数据。[data] 只在本次调用内被读取，调用返回后 Dart 侧可复用该缓冲。
///
/// # Safety
/// `h` 必须是 `pfm_blake3_new` 返回且尚未 free 的指针；
/// `data` 必须在 `len` 字节范围内可读。
#[no_mangle]
pub unsafe extern "C" fn pfm_blake3_update(
    h: *mut Hasher,
    data: *const u8,
    len: usize,
) -> i32 {
    if h.is_null() || (data.is_null() && len != 0) {
        return -1;
    }
    let hasher = &mut *h;
    let bytes: &[u8] = if len == 0 {
        &[]
    } else {
        slice::from_raw_parts(data, len)
    };
    let result = catch_unwind(AssertUnwindSafe(|| {
        if len >= PARALLEL_MIN_LEN {
            hasher.update_rayon(bytes);
        } else {
            hasher.update(bytes);
        }
    }));
    if result.is_ok() {
        0
    } else {
        -2
    }
}

/// 写出 32 字节摘要。句柄仍然有效，调用方随后要 `pfm_blake3_free`。
///
/// # Safety
/// `h` 必须是 `pfm_blake3_new` 返回且尚未 free 的指针；`out` 必须至少可写 `out_len` 字节。
#[no_mangle]
pub unsafe extern "C" fn pfm_blake3_final(h: *mut Hasher, out: *mut u8, out_len: usize) -> i32 {
    if h.is_null() || out.is_null() || out_len < DIGEST_LEN {
        return -1;
    }
    let hasher = &*h;
    let digest = catch_unwind(AssertUnwindSafe(|| hasher.finalize()));
    match digest {
        Ok(digest) => {
            std::ptr::copy_nonoverlapping(digest.as_bytes().as_ptr(), out, DIGEST_LEN);
            0
        }
        Err(_) => -2,
    }
}

/// 释放句柄。重复 free / 传空指针都会被忽略（空指针直接返回）。
///
/// # Safety
/// `h` 必须是 `pfm_blake3_new` 返回的指针，且同一指针只能 free 一次。
#[no_mangle]
pub unsafe extern "C" fn pfm_blake3_free(h: *mut Hasher) {
    if h.is_null() {
        return;
    }
    drop(Box::from_raw(h));
}

#[cfg(test)]
mod tests {
    use super::*;

    fn hash_chunks(chunks: &[&[u8]]) -> [u8; DIGEST_LEN] {
        let h = pfm_blake3_new();
        assert!(!h.is_null());
        for chunk in chunks {
            let rc = unsafe { pfm_blake3_update(h, chunk.as_ptr(), chunk.len()) };
            assert_eq!(rc, 0);
        }
        let mut out = [0u8; DIGEST_LEN];
        let rc = unsafe { pfm_blake3_final(h, out.as_mut_ptr(), out.len()) };
        assert_eq!(rc, 0);
        unsafe { pfm_blake3_free(h) };
        out
    }

    fn sample(len: usize) -> Vec<u8> {
        (0..len)
            .map(|i| ((i * 7 + (i >> 8)) & 0xff) as u8)
            .collect()
    }

    #[test]
    fn abi_version_is_stable() {
        assert_eq!(pfm_blake3_abi_version(), 1);
    }

    #[test]
    fn empty_input_matches_standard_vector() {
        // BLAKE3 空输入标准向量（与 Dart 侧 file_hasher_test 用的是同一个）
        assert_eq!(
            hex_lower(&hash_chunks(&[])),
            "af1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262"
        );
    }

    #[test]
    fn streaming_equals_one_shot_across_block_boundaries() {
        // 覆盖 chunk / 子树边界：BLAKE3 chunk 1024 字节、并行阈值 1 MiB
        let data = sample(3 * 1024 * 1024 + 12345);
        let one_shot = blake3::hash(&data);
        for block in [1usize, 1023, 1024, 1025, 65536, 1 << 20, 8 << 20] {
            let chunks: Vec<&[u8]> = data.chunks(block).collect();
            assert_eq!(
                hash_chunks(&chunks),
                *one_shot.as_bytes(),
                "block size {block} 的增量结果与一次性哈希不一致"
            );
        }
    }

    #[test]
    fn parallel_path_is_taken_for_large_blocks() {
        // 大于 PARALLEL_MIN_LEN 的块走 rayon，结果必须与串行路径一致
        let data = sample(2 * (1 << 20));
        let serial = blake3::hash(&data);
        let parallel = hash_chunks(&[&data]);
        assert_eq!(parallel, *serial.as_bytes());
    }

    #[test]
    fn null_and_bad_args_are_rejected_without_panicking() {
        let mut out = [0u8; DIGEST_LEN];
        unsafe {
            assert_eq!(pfm_blake3_update(std::ptr::null_mut(), std::ptr::null(), 0), -1);
            assert_eq!(pfm_blake3_final(std::ptr::null_mut(), out.as_mut_ptr(), out.len()), -1);
            pfm_blake3_free(std::ptr::null_mut()); // 不许崩
        }
        let h = pfm_blake3_new();
        unsafe {
            // 输出缓冲不够
            assert_eq!(pfm_blake3_final(h, out.as_mut_ptr(), DIGEST_LEN - 1), -1);
            // 长度为 0 时允许空指针
            assert_eq!(pfm_blake3_update(h, std::ptr::null(), 0), 0);
            assert_eq!(pfm_blake3_update(h, std::ptr::null(), 1), -1);
            pfm_blake3_free(h);
        }
    }

    #[test]
    fn json_hex_matches_blake3_crate() {
        let data = b"pfm";
        assert_eq!(hex_lower(&hash_chunks(&[data])), blake3::hash(data).to_hex().as_str());
    }

    fn hex_lower(bytes: &[u8]) -> String {
        bytes.iter().map(|b| format!("{b:02x}")).collect()
    }
}
