import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/api_endpoints.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/widgets/app_card.dart';

class PanelSettingsPage extends ConsumerStatefulWidget {
  const PanelSettingsPage({super.key});

  @override
  ConsumerState<PanelSettingsPage> createState() => _PanelSettingsPageState();
}

class _PanelSettingsPageState extends ConsumerState<PanelSettingsPage> {
  bool _loading = true;
  bool _saving = false;

  final _titleC = TextEditingController();
  final _iconC = TextEditingController();
  final _editorBgC = TextEditingController();
  final _logBgC = TextEditingController();

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _titleC.dispose();
    _iconC.dispose();
    _editorBgC.dispose();
    _logBgC.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    try {
      final resp = await DioClient.instance.dio.get(
        ApiEndpoints.panelSettings,
      );
      final data = extractData(resp.data);
      if (data is Map<String, dynamic>) {
        _titleC.text = data['panel_title']?.toString() ?? '';
        _iconC.text = data['panel_icon']?.toString() ?? '';
        _editorBgC.text = data['editor_background_color']?.toString() ?? '';
        _logBgC.text = data['log_background_color']?.toString() ?? '';
      }
      setState(() => _loading = false);
    } catch (_) {
      setState(() => _loading = false);
    }
  }

  Future<void> _save() async {
    setState(() => _saving = true);
    try {
      final data = <String, dynamic>{};
      if (_titleC.text.trim().isNotEmpty) {
        data['panel_title'] = _titleC.text.trim();
      }
      if (_iconC.text.trim().isNotEmpty) {
        data['panel_icon'] = _iconC.text.trim();
      }
      data['editor_background_color'] = _editorBgC.text.trim();
      data['log_background_color'] = _logBgC.text.trim();

      await DioClient.instance.dio.put(
        ApiEndpoints.panelSettings,
        data: data,
      );
      if (!mounted) return;
      AppGlassNotice.show(
        context,
        '面板设置已保存',
        type: AppGlassNoticeType.success,
      );
      _load();
    } catch (error) {
      if (!mounted) return;
      AppGlassNotice.show(
        context,
        extractErrorMessage(error, '保存面板设置失败'),
        type: AppGlassNoticeType.error,
      );
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final isLight = Theme.of(context).brightness == Brightness.light;
    

    return Scaffold(
      backgroundColor: Colors.transparent,
      body: Padding(
        padding: EdgeInsets.only(
          top: MediaQuery.of(context).padding.top + 12,
        ),
        child: Column(
          children: [
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20),
              child: Row(
                children: [
                  GestureDetector(
                    onTap: () => Navigator.of(context).pop(),
                    child: const Icon(Icons.arrow_back_ios, size: 20),
                  ),
                  const SizedBox(width: 8),
                  const Expanded(
                    child: Text(
                      '面板设置',
                      style: TextStyle(
                        fontSize: 24,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 16),
            Expanded(
              child: _loading
                  ? const Center(
                      child: CircularProgressIndicator(
                        color: AppColors.primary,
                      ),
                    )
                  : ListView(
                      padding: const EdgeInsets.fromLTRB(20, 0, 20, 100),
                      children: [
                        _buildTextField(
                          controller: _titleC,
                          label: '面板标题',
                          hint: '显示在页面标题栏',
                          isLight: isLight,
                        ),
                        const SizedBox(height: 12),
                        _buildTextField(
                          controller: _iconC,
                          label: '面板图标',
                          hint: '图标 URL 或路径',
                          isLight: isLight,
                        ),
                        const SizedBox(height: 24),
                        Text(
                          '界面配色',
                          style: TextStyle(
                            fontSize: 14,
                            fontWeight: FontWeight.w600,
                            color: isLight
                                ? AppColors.slate600
                                : AppColors.slate300,
                          ),
                        ),
                        const SizedBox(height: 8),
                        _buildTextField(
                          controller: _editorBgC,
                          label: '编辑器背景色',
                          hint: '例如 #1E1E1E 或 rgba(30,30,30,1)',
                          isLight: isLight,
                        ),
                        const SizedBox(height: 12),
                        _buildTextField(
                          controller: _logBgC,
                          label: '日志背景色',
                          hint: '例如 #000000 或 rgba(0,0,0,1)',
                          isLight: isLight,
                        ),
                        const SizedBox(height: 32),
                        SizedBox(
                          width: double.infinity,
                          height: 44,
                          child: AppLiquidGlassButton(
                            label: '保存设置',
                            onPressed: _saving ? null : _save,
                            width: double.infinity,
                            height: 44,
                            loading: _saving,
                            performanceMode: true,
                          ),
                        ),
                      ],
                    ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildTextField({
    required TextEditingController controller,
    required String label,
    String? hint,
    required bool isLight,
  }) {
    return AppCard(
      stableForScrolling: true,
      padding: const EdgeInsets.all(14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            label,
            style: TextStyle(
              fontSize: 12,
              fontWeight: FontWeight.w600,
              color: isLight ? AppColors.slate500 : AppColors.slate400,
            ),
          ),
          const SizedBox(height: 6),
          TextField(
            controller: controller,
            decoration: InputDecoration(
              hintText: hint,
              isDense: true,
              filled: false,
              border: InputBorder.none,
              contentPadding: EdgeInsets.zero,
            ),
          ),
        ],
      ),
    );
  }
}
