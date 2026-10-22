import 'package:flutter/material.dart';

/// 可缩放 / 可平移的图片视图。
///
/// 专门解决「放在 PageView 里时，放大后单指拖动会先被 PageView 抢走变成翻页」：
/// - 未放大时 `panEnabled` 为 false，单指横滑交给外层 PageView 翻页；
/// - 放大后 `panEnabled` 为 true，单指拖动只用于平移图片；
/// - 缩放状态通过 [onZoomChanged] 上报，外层据此把 PageView 的 physics 关掉
///   （否则即使这里吃到手势，PageView 仍可能在边界处接管）。
///
/// 另带双击放大 / 还原、手势结束后回弹到合法区域、放大时显示倍率与「重置」按钮。
class ZoomableImageView extends StatefulWidget {
  const ZoomableImageView({
    super.key,
    required this.child,
    this.isActive = true,
    this.minScale = 1.0,
    this.maxScale = 5.0,
    this.doubleTapScale = 2.5,
    this.resetTooltip,
    this.onZoomChanged,
  });

  /// 图片本体（通常是一个 `Image`）
  final Widget child;

  /// 是否是当前可见页：多图时只有当前页上报缩放状态，避免离屏页误锁翻页
  final bool isActive;

  final double minScale;
  final double maxScale;

  /// 双击放大到的倍率
  final double doubleTapScale;

  /// 放大后「重置」按钮的 tooltip（由外层传入本地化文案）
  final String? resetTooltip;

  /// 缩放状态变化（放大 / 回到默认），只在跨越阈值时回调一次
  final ValueChanged<bool>? onZoomChanged;

  @override
  State<ZoomableImageView> createState() => _ZoomableImageViewState();
}

class _ZoomableImageViewState extends State<ZoomableImageView>
    with SingleTickerProviderStateMixin {
  /// 倍率小于这个值视为「未放大」（浮点误差容忍）
  static const double _zoomThreshold = 1.01;

  final TransformationController _controller = TransformationController();
  late final AnimationController _animation = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 200),
  );

  Matrix4Tween? _matrixTween;
  Size _viewport = Size.zero;
  double _scale = 1.0;
  bool _reportedZoomed = false;
  Offset? _doubleTapPosition;

  bool get _isZoomed => _scale > _zoomThreshold;

  @override
  void initState() {
    super.initState();
    _controller.addListener(_handleTransformChanged);
    _animation.addListener(_applyAnimation);
  }

  @override
  void didUpdateWidget(ZoomableImageView oldWidget) {
    super.didUpdateWidget(oldWidget);
    // 翻到本页时把当前缩放状态重新上报一次（离屏期间外层可能已解锁翻页）。
    // didUpdateWidget 处在构建期间，这里同步回调会触发父级
    // 「setState() called during build」，必须挪到帧后。
    if (widget.isActive && !oldWidget.isActive && _isZoomed) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted || !widget.isActive) return;
        _reportZoom(force: true);
      });
    }
  }

  /// 只在越过阈值（或 [force]）时向父级上报一次，避免每帧都回调 setState
  void _reportZoom({bool force = false}) {
    if (!force && _isZoomed == _reportedZoomed) return;
    _reportedZoomed = _isZoomed;
    widget.onZoomChanged?.call(_isZoomed);
  }

  @override
  void dispose() {
    _controller.removeListener(_handleTransformChanged);
    _controller.dispose();
    _animation.dispose();
    super.dispose();
  }

  void _handleTransformChanged() {
    final scale = _controller.value.getMaxScaleOnAxis();
    if ((scale - _scale).abs() > 0.001) {
      setState(() => _scale = scale);
    }
    if (widget.isActive) _reportZoom();
  }

  void _applyAnimation() {
    final tween = _matrixTween;
    if (tween == null) return;
    _controller.value = tween.transform(_animation.value);
  }

  void _animateTo(Matrix4 target) {
    _matrixTween = Matrix4Tween(begin: _controller.value, end: target);
    _animation.forward(from: 0);
  }

  /// 双击：未放大时以点击点为中心放大，已放大时还原
  void _handleDoubleTap() {
    if (_isZoomed) {
      _animateTo(Matrix4.identity());
      return;
    }

    final scale = widget.doubleTapScale
        .clamp(widget.minScale, widget.maxScale)
        .toDouble();
    final focal = _doubleTapPosition ?? Offset(_viewport.width / 2, _viewport.height / 2);
    final matrix = Matrix4.identity()
      ..translateByDouble(
        focal.dx * (1 - scale),
        focal.dy * (1 - scale),
        0,
        1,
      )
      ..scaleByDouble(scale, scale, 1, 1);
    _animateTo(_clampTranslation(matrix, scale));
  }

  /// 手势结束后把图片收进合法区域：缩太小直接归位，拖出边界则回弹
  void _handleInteractionEnd(ScaleEndDetails details) {
    final matrix = _controller.value;
    final scale = matrix.getMaxScaleOnAxis();
    if (scale <= _zoomThreshold) {
      _animateTo(Matrix4.identity());
      return;
    }
    final clamped = _clampTranslation(matrix, scale);
    if (clamped != matrix) {
      _animateTo(clamped);
    }
  }

  /// 平移量限制在「图片边缘不离开视口」的范围内
  Matrix4 _clampTranslation(Matrix4 matrix, double scale) {
    final translation = matrix.getTranslation();
    final maxDx = (_viewport.width * (scale - 1)).clamp(0.0, double.infinity);
    final maxDy = (_viewport.height * (scale - 1)).clamp(0.0, double.infinity);
    final dx = translation.x.clamp(-maxDx, 0.0);
    final dy = translation.y.clamp(-maxDy, 0.0);
    if (dx == translation.x && dy == translation.y) return matrix;
    return Matrix4.identity()
      ..translateByDouble(dx, dy, 0, 1)
      ..scaleByDouble(scale, scale, 1, 1);
  }

  void _resetZoom() => _animateTo(Matrix4.identity());

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        _viewport = Size(constraints.maxWidth, constraints.maxHeight);
        return GestureDetector(
          onDoubleTapDown: (details) => _doubleTapPosition = details.localPosition,
          onDoubleTap: _handleDoubleTap,
          child: Stack(
            children: [
              Positioned.fill(
                child: InteractiveViewer(
                  transformationController: _controller,
                  minScale: widget.minScale,
                  maxScale: widget.maxScale,
                  // 未放大时禁用平移：手势留给外层 PageView 翻页
                  panEnabled: _isZoomed,
                  scaleEnabled: true,
                  onInteractionEnd: _handleInteractionEnd,
                  child: Center(child: widget.child),
                ),
              ),
              if (_isZoomed)
                Positioned(
                  bottom: 24,
                  left: 0,
                  right: 0,
                  child: Center(child: _buildZoomBadge(context)),
                ),
            ],
          ),
        );
      },
    );
  }

  Widget _buildZoomBadge(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
      decoration: BoxDecoration(
        color: Colors.black54,
        borderRadius: BorderRadius.circular(24),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            '${_scale.toStringAsFixed(1)}x',
            style: const TextStyle(
              color: Colors.white,
              fontSize: 13,
              fontWeight: FontWeight.w500,
            ),
          ),
          const SizedBox(width: 4),
          IconButton(
            icon: const Icon(Icons.fit_screen, color: Colors.white, size: 20),
            tooltip: widget.resetTooltip,
            visualDensity: VisualDensity.compact,
            onPressed: _resetZoom,
          ),
        ],
      ),
    );
  }
}
