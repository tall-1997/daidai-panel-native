import 'package:fl_chart/fl_chart.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/app_card.dart';

class TrendChart extends ConsumerWidget {
  final List<dynamic> data;

  const TrendChart({super.key, required this.data});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final isLight = theme.brightness == Brightness.light;
    

    final successSpots = <FlSpot>[];
    final failSpots = <FlSpot>[];
    final chartData = data
        .whereType<Map>()
        .map((item) => Map<String, dynamic>.from(item))
        .toList();

    for (int i = 0; i < chartData.length; i++) {
      final item = chartData[i];
      successSpots.add(
        FlSpot(i.toDouble(), _number(item['success'])),
      );
      failSpots.add(
        FlSpot(i.toDouble(), _number(item['failed'])),
      );
    }

    final chartContent = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Text(
              '近7天执行统计',
              style: TextStyle(
                fontSize: 13,
                fontWeight: FontWeight.w600,
                color: isLight ? AppColors.slate700 : AppColors.slate300,
              ),
            ),
            const Spacer(),
            _LegendDot(color: AppColors.primary, label: '成功', isLight: isLight),
            const SizedBox(width: 12),
            _LegendDot(color: AppColors.red500, label: '失败', isLight: isLight),
          ],
        ),
        const SizedBox(height: 16),
        SizedBox(
          height: 180,
          child: LineChart(
            LineChartData(
              gridData: FlGridData(
                show: true,
                drawVerticalLine: false,
                horizontalInterval: 5,
                getDrawingHorizontalLine: (value) => FlLine(
                  color: (isLight ? AppColors.slate200 : AppColors.slate800)
                      .withAlpha(120),
                  strokeWidth: 1,
                ),
              ),
              titlesData: FlTitlesData(
                leftTitles: AxisTitles(
                  sideTitles: SideTitles(
                    showTitles: true,
                    reservedSize: 32,
                    getTitlesWidget: (value, meta) => Text(
                      value.toInt().toString(),
                      style: TextStyle(
                        fontSize: 10,
                        color: isLight
                            ? AppColors.slate400
                            : AppColors.slate500,
                      ),
                    ),
                  ),
                ),
                bottomTitles: AxisTitles(
                  sideTitles: SideTitles(
                    showTitles: true,
                    interval: (chartData.length / 5).ceilToDouble().clamp(
                      1,
                      double.infinity,
                    ),
                    getTitlesWidget: (value, meta) {
                      final idx = value.toInt();
                      if (idx < 0 || idx >= chartData.length) {
                        return const SizedBox();
                      }
                      final item = chartData[idx];
                      final date = item['date']?.toString() ?? '';
                      return Padding(
                        padding: const EdgeInsets.only(top: 6),
                        child: Text(
                          date.length >= 5 ? date.substring(5) : date,
                          style: TextStyle(
                            fontSize: 10,
                            color: isLight
                                ? AppColors.slate400
                                : AppColors.slate500,
                          ),
                        ),
                      );
                    },
                  ),
                ),
                topTitles: const AxisTitles(
                  sideTitles: SideTitles(showTitles: false),
                ),
                rightTitles: const AxisTitles(
                  sideTitles: SideTitles(showTitles: false),
                ),
              ),
              borderData: FlBorderData(show: false),
              lineBarsData: [
                _line(successSpots, AppColors.primary),
                _line(failSpots, AppColors.red500),
              ],
            ),
          ),
        ),
      ],
    );

    return AppCard(
      padding: const EdgeInsets.all(16),
      child: chartContent,
    );
  }

  LineChartBarData _line(List<FlSpot> spots, Color color) {
    return LineChartBarData(
      spots: spots,
      isCurved: true,
      color: color,
      barWidth: 2,
      dotData: const FlDotData(show: false),
      belowBarData: BarAreaData(show: true, color: color.withAlpha(20)),
    );
  }

  double _number(dynamic value) {
    if (value is num) return value.toDouble();
    if (value is String) return double.tryParse(value.trim()) ?? 0;
    return 0;
  }
}

class _LegendDot extends StatelessWidget {
  final Color color;
  final String label;
  final bool isLight;

  const _LegendDot({
    required this.color,
    required this.label,
    required this.isLight,
  });

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Container(
          width: 8,
          height: 8,
          decoration: BoxDecoration(color: color, shape: BoxShape.circle),
        ),
        const SizedBox(width: 4),
        Text(
          label,
          style: TextStyle(
            fontSize: 11,
            color: isLight ? AppColors.slate500 : AppColors.slate400,
          ),
        ),
      ],
    );
  }
}
