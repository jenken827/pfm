import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../pages/pages.dart';

part 'login_route.g.dart';

@TypedGoRoute<LoginRoute>(
  path: '/login',
  routes: [TypedGoRoute<LoginCasdoorConfigRoute>(path: 'casdoor')],
)
@immutable
class LoginRoute extends GoRouteData with $LoginRoute {
  const LoginRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const LoginPage();
  }
}

@immutable
class LoginCasdoorConfigRoute extends GoRouteData
    with $LoginCasdoorConfigRoute {
  const LoginCasdoorConfigRoute();

  @override
  Widget build(BuildContext context, GoRouterState state) {
    return const CasdoorConfigPage();
  }
}
