import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../pages/pages.dart';

part 'transfer_route.g.dart';

/// 传输路由
@TypedGoRoute<TransferRoute>(path: '/transfer')
@immutable
class TransferRoute extends GoRouteData with $TransferRoute {
  const TransferRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const TransferPage();
  }
}
