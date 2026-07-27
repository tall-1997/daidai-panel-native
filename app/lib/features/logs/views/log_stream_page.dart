import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../../../core/network/api_endpoints.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/sse_client.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/models/task_log.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/utils/ansi_text.dart';
import '../../../shared/utils/log_background.dart';
import '../../../shared/widgets/app_card.dart';

class LogStreamPage extends StatefulWidget {
  final int logId;

  const LogStreamPage({super.key, required this.logId});

  @override
  State<LogStreamPage> createState() => _LogStreamPageState();
}

class _LogStreamPageState extends State<LogStreamPage> {
  final _sseClient = SseClient();
  final _scrollController = ScrollController();
  final _lines = <String>[];

  bool _loading = true;
  bool _done = false;
  bool _autoScroll = true;
  int? _taskId;
  String _status = '加载中...';
  Color? _logBackgroundColor;

  @override
  void initState() {
    super.initState();
    _loadAppearance();
    _loadLog();
  }

  Future<void> _loadAppearance() async {
    final color = await loadPanelLogBackgroundColor();
    if (!mounted) {
      return;
    }
    setState(() => _logBackgroundColor = color);
  }

  Future<void> _loadLog() async {
    setState(() {
      _loading = true;
      _status = '加载日志...';
    });

    try {
      final response = await DioClient.instance.dio.get(
        ApiEndpoints.logById(widget.logId),
      );
      final data = extractData(response.data);
      if (data is! Map) {
        throw StateError('Invalid log payload');
      }

      final payload = Map<String, dynamic>.from(data);

      final log = TaskLog.fromJson(payload);
      final content = payload['content']?.toString() ?? '';
      final historyLines = log.isRunning
          ? const <String>[]
          : _splitLines(content);

      if (!mounted) {
        return;
      }

      setState(() {
        _taskId = log.taskId;
        _lines
          ..clear()
          ..addAll(historyLines);
        _done = !log.isRunning;
        _loading = false;
        _status = log.isRunning ? '连接中...' : log.statusText;
      });
      if (_autoScroll && historyLines.isNotEmpty) {
        _scrollToBottom();
      }

      if (log.isRunning) {
        _connect();
      }
    } catch (_) {
      if (!mounted) {
        return;
      }
      setState(() {
        _loading = false;
        _done = true;
        _status = '加载失败';
      });
    }
  }

  void _connect() {
    final taskId = _taskId;
    if (taskId == null) {
      return;
    }

    _sseClient.connect(
      path: ApiEndpoints.logStream(taskId),
      autoReconnect: true,
      onEvent: (event) {
        if (!mounted) {
          return;
        }

        if (event.event == 'done') {
          setState(() {
            _done = true;
            _status = event.data == 'finished' ? '已完成' : event.data;
          });
          return;
        }

        final newLines = _splitLines(event.data);
        if (newLines.isEmpty) {
          return;
        }

        setState(() {
          _lines.addAll(newLines);
          _status = '运行中';
        });
        if (_autoScroll) {
          _scrollToBottom();
        }
      },
      onDone: () {
        if (!mounted) {
          return;
        }
        setState(() {
          _done = true;
          _status = '连接结束';
        });
      },
      onError: (_) {
        if (!mounted) {
          return;
        }
        setState(() {
          _done = true;
          _status = '连接错误';
        });
      },
    );
  }

  List<String> _splitLines(String content) {
    final normalized = content.replaceAll('\r\n', '\n');
    final lines = normalized.split('\n');
    if (lines.isNotEmpty && lines.last.isEmpty) {
      lines.removeLast();
    }
    return lines;
  }

  void _scrollToBottom() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_scrollController.hasClients) {
        _scrollController.animateTo(
          _scrollController.position.maxScrollExtent,
          duration: const Duration(milliseconds: 100),
          curve: Curves.easeOut,
        );
      }
    });
  }

  @override
  void dispose() {
    _sseClient.close();
    _scrollController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final logTheme = resolveLogSurfaceTheme(
      _logBackgroundColor,
      appBrightness: theme.brightness,
    );
    final chipBackground = logTheme.foreground.withAlpha(
      logTheme.brightness == Brightness.dark ? 24 : 14,
    );

    return Scaffold(
      backgroundColor: logTheme.background,
      appBar: AppBar(
        title: Text('日志 #${widget.logId}'),
        backgroundColor: logTheme.background,
        foregroundColor: logTheme.foreground,
        actions: [
          Padding(
            padding: const EdgeInsets.only(right: 8),
            child: Chip(
              backgroundColor: chipBackground,
              side: BorderSide(color: logTheme.foreground.withAlpha(32)),
              surfaceTintColor: Colors.transparent,
              label: Text(
                _status,
                style: TextStyle(fontSize: 12, color: logTheme.foreground),
              ),
              avatar: _done
                  ? Icon(Icons.check, size: 16, color: logTheme.foreground)
                  : SizedBox(
                      width: 14,
                      height: 14,
                      child: CircularProgressIndicator(
                        strokeWidth: 2,
                        color: logTheme.foreground,
                      ),
                    ),
              visualDensity: VisualDensity.compact,
            ),
          ),
          if (_lines.isNotEmpty)
            IconButton(
              icon: const Icon(Icons.copy),
              tooltip: '复制全部',
              onPressed: () {
                Clipboard.setData(ClipboardData(text: _lines.join('\n')));
                AppGlassNotice.show(
                  context,
                  '日志已复制到剪贴板',
                  type: AppGlassNoticeType.success,
                );
              },
            ),
          IconButton(
            icon: Icon(_autoScroll ? Icons.vertical_align_bottom : Icons.pause),
            tooltip: _autoScroll ? '自动滚动: 开' : '自动滚动: 关',
            onPressed: () {
              setState(() => _autoScroll = !_autoScroll);
              if (_autoScroll) {
                _scrollToBottom();
              }
            },
          ),
        ],
      ),
      body: Container(
        color: logTheme.background,
        child: _loading && _lines.isEmpty
            ? const Center(child: CircularProgressIndicator())
            : _lines.isEmpty
            ? Center(
                child: Text(
                  _done ? '无日志内容' : '等待日志...',
                  style: TextStyle(color: logTheme.mutedForeground),
                ),
              )
            : Theme(
                data: Theme.of(context).copyWith(
                  textSelectionTheme: TextSelectionThemeData(
                    selectionColor: AppColors.primary.withAlpha(80),
                    selectionHandleColor: AppColors.primary,
                  ),
                ),
                child: Scrollbar(
                  controller: _scrollController,
                  child: SingleChildScrollView(
                    controller: _scrollController,
                    padding: const EdgeInsets.all(12),
                    child: SelectableText.rich(
                      AnsiTextParser.buildTextSpan(
                        _lines.join('\n'),
                        baseStyle: TextStyle(
                          fontFamily: 'monospace',
                          fontSize: 13,
                          color: logTheme.foreground,
                          height: 1.5,
                        ),
                        brightness: logTheme.brightness,
                      ),
                      contextMenuBuilder: (context, editableTextState) {
                        return AdaptiveTextSelectionToolbar.editableText(
                          editableTextState: editableTextState,
                        );
                      },
                    ),
                  ),
                ),
              ),
      ),
    );
  }
}
