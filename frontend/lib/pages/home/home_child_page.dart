import 'package:flutter/material.dart';

class HomeChildPage extends StatelessWidget {
  const HomeChildPage({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Home Child Page')),
      body: const Center(child: Text('Welcome to the Home Child Page!')),
    );
  }
}
