import 'package:equatable/equatable.dart';
import 'package:pfm/proto/native/file.pb.dart';

enum FileSortType { type, name, size, modifiedTime, createdTime }

class FileSort extends Equatable {
  final FileSortType type;
  final bool asc;

  const FileSort({this.type = FileSortType.type, this.asc = true});

  void sort(List<File> list) {
    list.sort((a, b) {
      switch (type) {
        case FileSortType.name:
          return _compareString(a.name, b.name);
        case FileSortType.modifiedTime:
          return _compareNum(
            a.updatedAt.seconds as num? ?? 0,
            b.updatedAt.seconds as num? ?? 0,
          );
        case FileSortType.createdTime:
          return _compareNum(
            a.createdAt.seconds as num? ?? 0,
            b.createdAt.seconds as num? ?? 0,
          );

        case FileSortType.size:
          return _compareNum(a.size.toInt(), b.size.toInt());
        case FileSortType.type:
          final typeCompare = _compareNum(a.type.value, b.type.value);
          return typeCompare != 0
              ? typeCompare
              : _compareString(a.name, b.name);
      }
    });
  }

  // Add these helper methods in the bloc class
  int _compareString(String a, String b) =>
      asc ? a.compareTo(b) : b.compareTo(a);

  // int _compareDateTime(String? a, String? b) {
  //   final aval = DateTime.tryParse(a ?? '')?.microsecondsSinceEpoch ?? 0;
  //   final bval = DateTime.tryParse(b ?? '')?.microsecondsSinceEpoch ?? 0;

  //   return asc ? aval.compareTo(bval) : bval.compareTo(aval);
  // }

  int _compareNum(num? a, num? b) {
    final aVal = a ?? 0;
    final bVal = b ?? 0;
    return asc ? aVal.compareTo(bVal) : bVal.compareTo(aVal);
  }

  @override
  List<Object> get props => [asc, type];
}
