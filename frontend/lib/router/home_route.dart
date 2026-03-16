import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../pages/pages.dart';

part 'home_route.g.dart';

/// 首页路由
@TypedGoRoute<HomeRoute>(
  path: '/',
  routes: [
    TypedGoRoute<HomeChildRoute>(path: 'child'),
  ],
)
@immutable
class HomeRoute extends GoRouteData with $HomeRoute {
  const HomeRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const HomeIndexPage();
  }
}

@immutable
class HomeChildRoute extends GoRouteData with $HomeChildRoute {
  const HomeChildRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const HomeChildPage();
  }
}
