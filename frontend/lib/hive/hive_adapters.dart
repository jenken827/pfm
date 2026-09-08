import '../models/models.dart';
import 'package:flutter/material.dart';
import 'package:hive_ce_flutter/hive_ce_flutter.dart';
@GenerateAdapters([
  AdapterSpec<ThemeLanguage>(),
  AdapterSpec<ThemeMode>(),
  AdapterSpec<AuthToken>(),
  AdapterSpec<User>(),
])
part 'hive_adapters.g.dart';
