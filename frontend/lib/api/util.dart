import 'package:pfm/proto/native/common.pb.dart';
import 'package:pfm/proto/native/enum.pb.dart';
import 'package:pfm/services/logger.dart';
import 'package:protobuf/protobuf.dart';

/// 从响应数据中提取特定类型的数据（Isolate 中使用）
T extractRespData<T extends GeneratedMessage>(
  Map<String, dynamic> jsonData,
  T message,
) {
  final br = BaseResp();
  getLogger().debug('extractRespData: $jsonData');
  br.mergeFromProto3Json(jsonData, typeRegistry: TypeRegistry([message]));

  if (br.code != RespCode.SUCCESS) {
    throw Exception('API Error: ${br.code.value} - ${br.message}');
  }

  if (!br.hasData()) {
    return message;
  }

  final result = message;
  br.data.unpackInto(result);
  return result;
}
