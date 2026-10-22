import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pfm/widgets/zoomable_image_view.dart';

/// 单图：直接把 ZoomableImageView 塞满一个固定尺寸的容器
Widget _single({ValueChanged<bool>? onZoomChanged, bool isActive = true}) {
  return MaterialApp(
    home: Scaffold(
      body: Center(
        child: SizedBox(
          width: 400,
          height: 600,
          child: ZoomableImageView(
            isActive: isActive,
            resetTooltip: 'reset',
            onZoomChanged: onZoomChanged,
            child: const ColoredBox(color: Colors.blue),
          ),
        ),
      ),
    ),
  );
}

/// 图集：与 `_FilePreviewPageState._buildImagePreview` 相同的接线方式
/// （放大回调 → 关掉 PageView 的 physics）
class _Gallery extends StatefulWidget {
  const _Gallery();

  @override
  State<_Gallery> createState() => _GalleryState();
}

class _GalleryState extends State<_Gallery> {
  final PageController _controller = PageController();
  int _page = 0;
  bool _zoomed = false;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      home: Scaffold(
        body: PageView.builder(
          controller: _controller,
          itemCount: 3,
          physics: _zoomed
              ? const NeverScrollableScrollPhysics()
              : const PageScrollPhysics(),
          onPageChanged: (index) => setState(() {
            _page = index;
            _zoomed = false;
          }),
          itemBuilder: (context, index) {
            return ZoomableImageView(
              key: ValueKey('img-$index'),
              isActive: index == _page,
              onZoomChanged: (zoomed) {
                if (index != _page) return;
                setState(() => _zoomed = zoomed);
              },
              child: ColoredBox(
                color: index.isEven ? Colors.blue : Colors.green,
                child: Center(child: Text('page $index')),
              ),
            );
          },
        ),
      ),
    );
  }
}

/// 模拟一次"人手"横滑：分多步移动（每步小于 kPanSlop），让 PageView 的拖拽
/// 识别器先于 InteractiveViewer 的缩放识别器接管手势——真实手指就是这样，
/// 而 `tester.drag`/`fling` 一步跨过两种 slop，不能用来判断翻页。
Future<void> _swipe(WidgetTester tester, double dx) async {
  final gesture = await tester.startGesture(
    tester.getCenter(find.byType(PageView)),
  );
  const steps = 20;
  for (var i = 0; i < steps; i++) {
    await gesture.moveBy(Offset(dx / steps, 0));
    await tester.pump(const Duration(milliseconds: 16));
  }
  await gesture.up();
  await tester.pumpAndSettle();
}

/// 模拟一次双击（间隔小于 kDoubleTapTimeout）
Future<void> _doubleTap(WidgetTester tester, Finder finder) async {
  await tester.tap(finder);
  await tester.pump(const Duration(milliseconds: 60));
  await tester.tap(finder);
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('双击放大到 2.5x，再双击回到 1x', (tester) async {
    final zoomEvents = <bool>[];
    await tester.pumpWidget(_single(onZoomChanged: zoomEvents.add));
    await tester.pumpAndSettle();

    expect(find.text('2.5x'), findsNothing);

    await _doubleTap(tester, find.byType(ZoomableImageView));

    expect(zoomEvents, [true]);
    expect(find.text('2.5x'), findsOneWidget);

    await _doubleTap(tester, find.byType(ZoomableImageView));

    expect(zoomEvents, [true, false]);
    expect(find.text('2.5x'), findsNothing);
  });

  testWidgets('未放大时禁用平移，放大后启用', (tester) async {
    await tester.pumpWidget(_single());
    await tester.pumpAndSettle();

    InteractiveViewer viewer() =>
        tester.widget<InteractiveViewer>(find.byType(InteractiveViewer));

    expect(viewer().panEnabled, isFalse);
    expect(viewer().scaleEnabled, isTrue);

    await _doubleTap(tester, find.byType(ZoomableImageView));

    expect(viewer().panEnabled, isTrue);
  });

  testWidgets('离屏页不上报缩放状态', (tester) async {
    final zoomEvents = <bool>[];
    await tester.pumpWidget(
      _single(isActive: false, onZoomChanged: zoomEvents.add),
    );
    await tester.pumpAndSettle();

    await _doubleTap(tester, find.byType(ZoomableImageView));

    expect(zoomEvents, isEmpty);
  });

  testWidgets('切回前台时补报仍处于放大，且不触发构建期 setState', (tester) async {
    var active = false;
    final zoomEvents = <bool>[];
    late StateSetter setOuter;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: StatefulBuilder(
            builder: (context, setState) {
              setOuter = setState;
              return SizedBox(
                width: 400,
                height: 600,
                child: ZoomableImageView(
                  isActive: active,
                  onZoomChanged: zoomEvents.add,
                  child: const ColoredBox(color: Colors.blue),
                ),
              );
            },
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await _doubleTap(tester, find.byType(ZoomableImageView));
    expect(zoomEvents, isEmpty, reason: '离屏期间不上报');
    expect(tester.takeException(), isNull);

    setOuter(() => active = true);
    await tester.pumpAndSettle();

    expect(tester.takeException(), isNull);
    expect(zoomEvents, [true]);
  });

  testWidgets('未放大时横滑可以翻页', (tester) async {
    await tester.pumpWidget(const _Gallery());
    await tester.pumpAndSettle();

    expect(find.text('page 0'), findsOneWidget);

    await _swipe(tester, -500);

    expect(find.text('page 1'), findsOneWidget);
  });

  testWidgets('放大后横滑不会翻页（手势留给图片平移）', (tester) async {
    await tester.pumpWidget(const _Gallery());
    await tester.pumpAndSettle();

    await _doubleTap(tester, find.byType(ZoomableImageView).first);

    final pageView = tester.widget<PageView>(find.byType(PageView));
    expect(pageView.physics, isA<NeverScrollableScrollPhysics>());

    await _swipe(tester, -500);

    expect(find.text('page 0'), findsOneWidget);
    expect(find.text('page 1'), findsNothing);
  });

  testWidgets('放大后重置再横滑，翻页恢复', (tester) async {
    await tester.pumpWidget(const _Gallery());
    await tester.pumpAndSettle();

    await _doubleTap(tester, find.byType(ZoomableImageView).first);
    await _doubleTap(tester, find.byType(ZoomableImageView).first);

    expect(
      tester.widget<PageView>(find.byType(PageView)).physics,
      isA<PageScrollPhysics>(),
    );

    await _swipe(tester, -500);

    expect(find.text('page 1'), findsOneWidget);
  });
}
