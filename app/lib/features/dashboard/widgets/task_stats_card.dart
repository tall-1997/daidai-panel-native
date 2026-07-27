import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/app_card.dart';

class TaskStatsCard extends ConsumerWidget {
  final int total;
  final int enabled;
  final int running;
  final int disabled;
  final int todaySuccess;
  final int todayFailed;
  final VoidCallback? onTap;

  const TaskStatsCard({
    super.key,
    required this.total,
    required this.enabled,
    required this.running,
    required this.disabled,
    required this.todaySuccess,
    required this.todayFailed,
    this.onTap,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final isLight = theme.brightness == Brightness.light;
    

    final content = Column(
      children: [
        Row(
          children: [
            _StatItem(
              label: '总任务',
              value: '$total',
              color: AppColors.primary,
              isLight: isLight,
            ),
            _StatItem(
              label: '已启用',
              value: '$enabled',
              color: AppColors.primary,
              isLight: isLight,
            ),
            _StatItem(
              label: '运行中',
              value: '$running',
              color: AppColors.blue500,
              isLight: isLight,
            ),
            _StatItem(
              label: '已禁用',
              value: '$disabled',
              color: AppColors.slate400,
              isLight: isLight,
            ),
          ],
        ),
        Padding(
          padding: const EdgeInsets.symmetric(vertical: 12),
          child: Divider(
            height: 1,
            color: isLight ? AppColors.glassDivider : AppColors.slate800,
          ),
        ),
        Row(
          children: [
            _StatItem(
              label: '今日成功',
              value: '$todaySuccess',
              color: AppColors.primary,
              isLight: isLight,
            ),
            _StatItem(
              label: '今日失败',
              value: '$todayFailed',
              color: AppColors.red500,
              isLight: isLight,
            ),
          ],
        ),
      ],
    );

    return GestureDetector(
      onTap: onTap,
      child: AppCard(
        padding: const EdgeInsets.all(16),
        child: content,
      ),
    );
  }
}

class _StatItem extends StatelessWidget {
  final String label;
  final String value;
  final Color color;
  final bool isLight;

  const _StatItem({
    required this.label,
    required this.value,
    required this.color,
    required this.isLight,
  });

  @override
  Widget build(BuildContext context) {
    return Expanded(
      child: Column(
        children: [
          Text(
            value,
            style: TextStyle(
              fontSize: 22,
              fontWeight: FontWeight.w700,
              color: color,
            ),
          ),
          const SizedBox(height: 4),
          Text(
            label,
            style: TextStyle(
              fontSize: 11,
              color: isLight ? AppColors.slate500 : AppColors.slate400,
            ),
          ),
        ],
      ),
    );
  }
}
