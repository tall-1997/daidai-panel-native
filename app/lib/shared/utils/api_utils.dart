/// 从 API 响应中提取 data 字段
/// 后端格式：response.Success() 直接输出，某些接口用 gin.H{"data": ...} 包了一层
dynamic extractData(dynamic responseData) {
  if (responseData is Map<String, dynamic> &&
      responseData.containsKey('data')) {
    return responseData['data'];
  }
  return responseData;
}

/// 从分页响应中提取列表和总数
/// 后端 response.Paginated() 格式: {data: [...], total: N, page: N, page_size: N}
({List<Map<String, dynamic>> items, int total}) extractPaginated(
  dynamic responseData,
) {
  if (responseData is Map<String, dynamic>) {
    final dataField = responseData['data'];
    // {data: [...], total: N} — 标准分页格式
    if (dataField is List) {
      final items = dataField.whereType<Map<String, dynamic>>().toList();
      final total = _toInt(responseData['total']) ?? items.length;
      return (items: items, total: total);
    }
    // 兜底：{data: {data: [...], total: N}}
    if (dataField is Map<String, dynamic>) {
      final innerList = dataField['data'];
      if (innerList is List) {
        final items = innerList.whereType<Map<String, dynamic>>().toList();
        final total = _toInt(dataField['total']) ?? items.length;
        return (items: items, total: total);
      }
    }
  }
  // 直接是列表
  if (responseData is List) {
    final items = responseData.whereType<Map<String, dynamic>>().toList();
    return (items: items, total: items.length);
  }
  return (items: <Map<String, dynamic>>[], total: 0);
}

/// 从 API 错误响应中提取可读的错误信息
/// 兼容 Dio 异常和一般异常，优先返回后端返回的 error/message 字段
String extractErrorMessage(dynamic error, String fallback) {
  try {
    final data = (error as dynamic).response?.data;
    if (data is Map) {
      final msg = data['error'] ?? data['message'];
      if (msg != null && msg.toString().trim().isNotEmpty) {
        return msg.toString().trim();
      }
    }
  } catch (_) {}
  try {
    final message = (error as dynamic).message;
    if (message is String && message.trim().isNotEmpty) {
      return message.trim();
    }
  } catch (_) {}
  return fallback;
}

String extractScriptSaveErrorMessage(dynamic error, String fallback) {
  final raw = extractErrorMessage(error, fallback).trim();
  if (raw.isEmpty) {
    return fallback;
  }

  if (raw.contains('当前路径是目录')) {
    return '当前选中的是目录，不是可编辑脚本文件';
  }
  if (raw.contains('文件不存在')) {
    return '脚本不存在，可能已被删除、重命名或移动';
  }
  if (raw.contains('不允许路径穿越') ||
      raw.contains('检测到路径穿越') ||
      raw.contains('路径包含非法字符')) {
    return '脚本路径无效，请刷新脚本树后重试';
  }
  if (raw.contains('二进制') || raw.contains('binary')) {
    return '当前文件是二进制内容，暂不支持在线保存';
  }
  if (raw.contains('写入文件失败') || raw.contains('创建目标目录失败')) {
    return '$raw，请检查面板数据目录挂载和写入权限';
  }
  if (raw.contains('ERR_REQUIRE_ESM') ||
      (raw.contains('ES Module') && raw.contains('require()'))) {
    return '依赖已安装，但当前模块是 ESM 格式，脚本仍在使用 require() 加载，请改用 import() 或安装兼容旧写法的版本';
  }

  return raw;
}

/// 安全转 int
int? _toInt(dynamic v) {
  if (v == null) return null;
  if (v is int) return v;
  if (v is num) return v.toInt();
  return int.tryParse(v.toString());
}
