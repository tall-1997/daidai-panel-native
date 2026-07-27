import 'package:flutter/material.dart';
import 'app_card.dart';

class AppAsyncState extends StatelessWidget {
  final bool loading;
  final String? error;
  final bool empty;
  final String emptyText;
  final VoidCallback onRetry;
  final Widget child;
  const AppAsyncState({super.key,required this.loading,required this.error,required this.empty,required this.emptyText,required this.onRetry,required this.child});
  @override Widget build(BuildContext context){
    if(loading)return const Center(child:CircularProgressIndicator());
    if(error!=null)return Center(child:AppCard(child:Column(mainAxisSize:MainAxisSize.min,children:[const Icon(Icons.cloud_off_outlined,size:42),const SizedBox(height:10),Text(error!,textAlign:TextAlign.center),const SizedBox(height:12),AppLiquidGlassButton(label:'重试',onPressed:onRetry)])));
    if(empty)return Center(child:Text(emptyText,style:TextStyle(color:Theme.of(context).colorScheme.onSurfaceVariant)));
    return child;
  }
}
