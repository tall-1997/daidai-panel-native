import 'dart:io';

import 'package:dio/dio.dart';
import 'package:file_picker/file_picker.dart';
import 'package:path_provider/path_provider.dart';

import '../../shared/models/raw_log_ticket.dart';
import '../network/dio_client.dart';

class RawLogDownloadService {
  static final Set<String> _reservedPaths = <String>{};
  static const _ticketSafetyWindow = Duration(seconds: 5);

  static Future<String> download({
    required String ticketPath,
    Map<String, dynamic>? queryParameters,
  }) => saveToDocuments(
    ticketPath: ticketPath,
    queryParameters: queryParameters,
  );

  static Future<String> saveToDocuments({
    required String ticketPath,
    Map<String, dynamic>? queryParameters,
  }) async {
    final ticket = await _requestTicket(ticketPath, queryParameters);
    final documentsDirectory = await getApplicationDocumentsDirectory();
    final downloadDirectory = Directory(
      '${documentsDirectory.path}${Platform.pathSeparator}downloads',
    );
    await downloadDirectory.create(recursive: true);
    final targetPath = await _availablePath(
      downloadDirectory.path,
      _safeFilename(ticket.filename),
    );
    try {
      return await _downloadWithRefresh(
        ticket: ticket,
        ticketPath: ticketPath,
        queryParameters: queryParameters,
        targetPath: targetPath,
      );
    } finally {
      _reservedPaths.remove(targetPath);
    }
  }

  static Future<String?> export({
    required String ticketPath,
    Map<String, dynamic>? queryParameters,
  }) async {
    final ticket = await _requestTicket(ticketPath, queryParameters);
    final temporaryDirectory = await getTemporaryDirectory();
    final temporaryPath = await _availablePath(
      temporaryDirectory.path,
      _safeFilename(ticket.filename),
    );
    try {
      await _downloadWithRefresh(
        ticket: ticket,
        ticketPath: ticketPath,
        queryParameters: queryParameters,
        targetPath: temporaryPath,
      );
      return FilePicker.platform.saveFile(
        dialogTitle: '导出原始日志',
        fileName: _safeFilename(ticket.filename),
        type: FileType.any,
        bytes: await File(temporaryPath).readAsBytes(),
      );
    } finally {
      _reservedPaths.remove(temporaryPath);
      await _deleteIfExists(temporaryPath);
      await _deleteIfExists('$temporaryPath.part');
    }
  }

  static Future<RawLogTicket> _requestTicket(
    String ticketPath,
    Map<String, dynamic>? queryParameters,
  ) async {
    final response = await DioClient.instance.dio.get(
      ticketPath,
      queryParameters: queryParameters,
    );
    return RawLogTicket.fromResponse(response.data);
  }

  static Future<String> _downloadWithRefresh({
    required RawLogTicket ticket,
    required String ticketPath,
    required Map<String, dynamic>? queryParameters,
    required String targetPath,
  }) async {
    var currentTicket = ticket;
    var refreshed = false;
    if (currentTicket.isExpired(safetyWindow: _ticketSafetyWindow)) {
      currentTicket = await _requestTicket(ticketPath, queryParameters);
      refreshed = true;
    }

    try {
      while (true) {
        try {
          await _downloadTicket(currentTicket, targetPath);
          return targetPath;
        } on DioException catch (error) {
          if (refreshed ||
              !shouldRefreshRawLogTicket(error.response?.statusCode)) {
            rethrow;
          }
          currentTicket = await _requestTicket(ticketPath, queryParameters);
          refreshed = true;
        }
      }
    } finally {
      await _deleteIfExists('$targetPath.part');
    }
  }

  static Future<void> _downloadTicket(
    RawLogTicket ticket,
    String targetPath,
  ) async {
    final temporaryPath = '$targetPath.part';
    await _deleteIfExists(temporaryPath);
    final ticketUri = Uri.parse(ticket.url);
    final resolvedUrl = ticketUri.hasScheme
        ? ticketUri.toString()
        : Uri.parse('${DioClient.instance.baseUrl}/')
              .resolveUri(ticketUri)
              .toString();
    final downloadDio = DioClient.instance.rawDio;
    try {
      await downloadDio.download(
        resolvedUrl,
        temporaryPath,
        options: Options(receiveTimeout: const Duration(minutes: 10)),
        deleteOnError: true,
      );
      await File(temporaryPath).rename(targetPath);
    } finally {
      downloadDio.close(force: true);
    }
  }

  static Future<void> _deleteIfExists(String path) async {
    final file = File(path);
    if (await file.exists()) await file.delete();
  }

  static Future<String> _availablePath(String directory, String filename) async {
    final separator = Platform.pathSeparator;
    final dot = filename.lastIndexOf('.');
    final basename = dot > 0 ? filename.substring(0, dot) : filename;
    final extension = dot > 0 ? filename.substring(dot) : '';
    var suffix = 1;
    var candidate = '$directory$separator$filename';
    while (true) {
      if (_reservedPaths.contains(candidate) || await File(candidate).exists()) {
        candidate = '$directory$separator$basename-$suffix$extension';
        suffix++;
        continue;
      }
      // Another download can reserve the same path while File.exists awaits.
      if (_reservedPaths.contains(candidate)) continue;
      _reservedPaths.add(candidate);
      return candidate;
    }
  }

  static String _safeFilename(String filename) {
    final sanitized = filename
        .replaceAll(RegExp(r'[\\/:*?"<>|\x00-\x1F]'), '_')
        .trim();
    if (sanitized.isEmpty || sanitized == '.' || sanitized == '..') {
      return 'raw.log';
    }
    return sanitized.length > 180 ? sanitized.substring(0, 180) : sanitized;
  }
}

bool shouldRefreshRawLogTicket(int? statusCode) =>
    statusCode == 401 ||
    statusCode == 403 ||
    statusCode == 404 ||
    statusCode == 410;
