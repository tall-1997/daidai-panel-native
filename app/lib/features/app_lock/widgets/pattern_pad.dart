import 'package:flutter/material.dart';

import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/app_card.dart';

class PatternPad extends StatelessWidget {
  const PatternPad({
    super.key,
    required this.selectedPoints,
    required this.onPointTap,
    required this.onClear,
    required this.onBackspace,
    this.title,
    this.subtitle,
  });

  final List<int> selectedPoints;
  final ValueChanged<int> onPointTap;
  final VoidCallback onClear;
  final VoidCallback onBackspace;
  final String? title;
  final String? subtitle;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (title != null)
          Text(
            title!,
            style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w700),
            textAlign: TextAlign.center,
          ),
        if (subtitle != null) ...[
          const SizedBox(height: 4),
          Text(
            subtitle!,
            style: TextStyle(
              fontSize: 12,
              color: theme.colorScheme.onSurfaceVariant,
            ),
            textAlign: TextAlign.center,
          ),
        ],
        const SizedBox(height: 16),
        AspectRatio(
          aspectRatio: 1,
          child: GridView.builder(
            physics: const NeverScrollableScrollPhysics(),
            gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
              crossAxisCount: 3,
              crossAxisSpacing: 12,
              mainAxisSpacing: 12,
            ),
            itemCount: 9,
            itemBuilder: (context, index) {
              final point = index + 1;
              final selectedIndex = selectedPoints.indexOf(point);
              final isSelected = selectedIndex >= 0;

              return AppLiquidGlassSurface(
                borderRadius: 999,
                onTap: () => onPointTap(point),
                accentColor: AppColors.primary,
                selected: isSelected,
                performanceMode: true,
                child: AspectRatio(
                  aspectRatio: 1,
                  child: Center(
                    child: Text(
                      isSelected ? '${selectedIndex + 1}' : '',
                      style: const TextStyle(
                        color: AppColors.primary,
                        fontSize: 18,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                ),
              );
            },
          ),
        ),
        const SizedBox(height: 12),
        Row(
          children: [
            Expanded(
              child: OutlinedButton.icon(
                onPressed: selectedPoints.isEmpty ? null : onBackspace,
                icon: const Icon(Icons.backspace_outlined, size: 18),
                label: const Text('撤回'),
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: OutlinedButton.icon(
                onPressed: selectedPoints.isEmpty ? null : onClear,
                icon: const Icon(Icons.refresh_outlined, size: 18),
                label: const Text('清空'),
              ),
            ),
          ],
        ),
      ],
    );
  }
}
