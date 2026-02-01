#!/usr/bin/env python3
"""
日志分析工具 - 高级版
支持按级别、时间段、关键词过滤，生成统计报告
"""

import os
import re
import sys
from datetime import datetime, timedelta
from collections import defaultdict, Counter
from typing import List, Dict, Tuple
import argparse

# ANSI 颜色代码
class Colors:
    RED = '\033[0;31m'
    YELLOW = '\033[1;33m'
    GREEN = '\033[0;32m'
    BLUE = '\033[0;34m'
    CYAN = '\033[0;36m'
    MAGENTA = '\033[0;35m'
    BOLD = '\033[1m'
    NC = '\033[0m'  # No Color

class LogAnalyzer:
    def __init__(self, log_dir: str = './logs'):
        self.log_dir = log_dir
        self.level_colors = {
            'ERROR': Colors.RED,
            'WARN': Colors.YELLOW,
            'INFO': Colors.GREEN,
            'DEBUG': Colors.CYAN,
        }
    
    def get_log_files(self, date: str = None) -> List[str]:
        """获取指定日期的日志文件"""
        if date is None:
            date = datetime.now().strftime('%Y-%m-%d')
        
        app_log = os.path.join(self.log_dir, f'app-quantmesh-{date}.log')
        web_log = os.path.join(self.log_dir, f'web-gin-{date}.log')
        
        files = []
        if os.path.exists(app_log):
            files.append(app_log)
        if os.path.exists(web_log):
            files.append(web_log)
        
        return files
    
    def get_available_dates(self) -> List[str]:
        """获取所有可用的日志日期"""
        dates = set()
        if not os.path.exists(self.log_dir):
            return []
        
        for filename in os.listdir(self.log_dir):
            match = re.match(r'app-quantmesh-(\d{4}-\d{2}-\d{2})\.log', filename)
            if match:
                dates.add(match.group(1))
        
        return sorted(list(dates), reverse=True)
    
    def parse_log_line(self, line: str) -> Dict:
        """解析日志行"""
        # 日志格式: YYYY/MM/DD HH:MM:SS [LEVEL] 消息
        pattern = r'(\d{4}/\d{2}/\d{2}\s+\d{2}:\d{2}:\d{2})\s+\[([^\]]+)\]\s*(.*)'
        match = re.match(pattern, line)
        
        if match:
            return {
                'timestamp': match.group(1),
                'level': match.group(2),
                'message': match.group(3),
                'raw': line
            }
        return None
    
    def filter_logs(self, files: List[str], level: str = None, 
                   keyword: str = None, start_time: str = None, 
                   end_time: str = None) -> List[Dict]:
        """过滤日志"""
        results = []
        
        for log_file in files:
            if not os.path.exists(log_file):
                continue
            
            with open(log_file, 'r', encoding='utf-8', errors='ignore') as f:
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    
                    parsed = self.parse_log_line(line)
                    if not parsed:
                        continue
                    
                    # 级别过滤
                    if level and parsed['level'] != level:
                        continue
                    
                    # 关键词过滤
                    if keyword and keyword.lower() not in parsed['message'].lower():
                        continue
                    
                    # 时间过滤（简化版）
                    # TODO: 实现更精确的时间过滤
                    
                    results.append(parsed)
        
        return results
    
    def generate_statistics(self, date: str = None) -> Dict:
        """生成统计信息"""
        files = self.get_log_files(date)
        if not files:
            return {}
        
        stats = {
            'total': 0,
            'by_level': Counter(),
            'by_file': {},
            'top_errors': [],
            'top_warnings': [],
        }
        
        error_messages = []
        warning_messages = []
        
        for log_file in files:
            filename = os.path.basename(log_file)
            file_stats = Counter()
            
            if not os.path.exists(log_file):
                continue
            
            with open(log_file, 'r', encoding='utf-8', errors='ignore') as f:
                for line in f:
                    parsed = self.parse_log_line(line)
                    if not parsed:
                        continue
                    
                    stats['total'] += 1
                    level = parsed['level']
                    stats['by_level'][level] += 1
                    file_stats[level] += 1
                    
                    # 收集错误和警告消息
                    if level == 'ERROR':
                        error_messages.append(parsed['message'])
                    elif level == 'WARN':
                        warning_messages.append(parsed['message'])
            
            stats['by_file'][filename] = dict(file_stats)
        
        # 统计最常见的错误和警告
        stats['top_errors'] = Counter(error_messages).most_common(10)
        stats['top_warnings'] = Counter(warning_messages).most_common(10)
        
        return stats
    
    def print_colored(self, text: str, color: str):
        """打印彩色文本"""
        print(f"{color}{text}{Colors.NC}")
    
    def print_logs(self, logs: List[Dict], limit: int = None):
        """打印日志"""
        if limit:
            logs = logs[-limit:]
        
        for log in logs:
            color = self.level_colors.get(log['level'], Colors.NC)
            self.print_colored(log['raw'], color)
    
    def print_statistics(self, stats: Dict, date: str):
        """打印统计信息"""
        self.print_colored('=' * 60, Colors.CYAN)
        self.print_colored(f'日志统计报告 - {date}', Colors.BOLD)
        self.print_colored('=' * 60, Colors.CYAN)
        print()
        
        # 总体统计
        print(f"📊 总日志数: {stats['total']}")
        print()
        
        # 按级别统计
        self.print_colored('按级别统计:', Colors.BOLD)
        for level in ['ERROR', 'WARN', 'INFO', 'DEBUG']:
            count = stats['by_level'].get(level, 0)
            color = self.level_colors.get(level, Colors.NC)
            percentage = (count / stats['total'] * 100) if stats['total'] > 0 else 0
            self.print_colored(f"  {level:8s}: {count:6d} ({percentage:5.2f}%)", color)
        print()
        
        # 按文件统计
        self.print_colored('按文件统计:', Colors.BOLD)
        for filename, file_stats in stats['by_file'].items():
            print(f"\n  📄 {filename}")
            for level, count in sorted(file_stats.items()):
                color = self.level_colors.get(level, Colors.NC)
                self.print_colored(f"    {level:8s}: {count}", color)
        print()
        
        # Top 错误
        if stats['top_errors']:
            self.print_colored('🔴 最常见的错误 (Top 10):', Colors.RED)
            for i, (msg, count) in enumerate(stats['top_errors'], 1):
                # 截断过长的消息
                short_msg = msg[:100] + '...' if len(msg) > 100 else msg
                print(f"  {i:2d}. [{count:3d}次] {short_msg}")
            print()
        
        # Top 警告
        if stats['top_warnings']:
            self.print_colored('⚠️  最常见的警告 (Top 10):', Colors.YELLOW)
            for i, (msg, count) in enumerate(stats['top_warnings'], 1):
                short_msg = msg[:100] + '...' if len(msg) > 100 else msg
                print(f"  {i:2d}. [{count:3d}次] {short_msg}")
            print()
        
        self.print_colored('=' * 60, Colors.CYAN)
    
    def generate_report(self, start_date: str, end_date: str, output_file: str = None):
        """生成时间段报告"""
        start = datetime.strptime(start_date, '%Y-%m-%d')
        end = datetime.strptime(end_date, '%Y-%m-%d')
        
        total_stats = {
            'dates': [],
            'total_logs': 0,
            'by_level': Counter(),
            'all_errors': [],
            'all_warnings': [],
        }
        
        current = start
        while current <= end:
            date_str = current.strftime('%Y-%m-%d')
            stats = self.generate_statistics(date_str)
            
            if stats:
                total_stats['dates'].append(date_str)
                total_stats['total_logs'] += stats['total']
                total_stats['by_level'].update(stats['by_level'])
                total_stats['all_errors'].extend([msg for msg, _ in stats['top_errors']])
                total_stats['all_warnings'].extend([msg for msg, _ in stats['top_warnings']])
            
            current += timedelta(days=1)
        
        # 打印报告
        self.print_colored('=' * 60, Colors.CYAN)
        self.print_colored(f'时间段报告: {start_date} 至 {end_date}', Colors.BOLD)
        self.print_colored('=' * 60, Colors.CYAN)
        print()
        
        print(f"📅 覆盖日期: {', '.join(total_stats['dates'])}")
        print(f"📊 总日志数: {total_stats['total_logs']}")
        print()
        
        self.print_colored('按级别汇总:', Colors.BOLD)
        for level in ['ERROR', 'WARN', 'INFO', 'DEBUG']:
            count = total_stats['by_level'].get(level, 0)
            color = self.level_colors.get(level, Colors.NC)
            self.print_colored(f"  {level:8s}: {count:6d}", color)
        
        print()
        self.print_colored('=' * 60, Colors.CYAN)

def main():
    parser = argparse.ArgumentParser(
        description='日志分析工具 - 按级别、关键词过滤和统计日志',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  %(prog)s --stats                          # 显示今天的统计
  %(prog)s --level ERROR                    # 查看今天的错误
  %(prog)s --level WARN --date 2026-01-20   # 查看指定日期的警告
  %(prog)s --keyword "保证金"                # 搜索包含"保证金"的日志
  %(prog)s --level ERROR --tail 50          # 显示最后50条错误
  %(prog)s --report --from 2026-01-15 --to 2026-01-21  # 生成时间段报告
  %(prog)s --list                           # 列出所有可用的日志日期
        """
    )
    
    parser.add_argument('--level', choices=['ERROR', 'WARN', 'INFO', 'DEBUG'],
                       help='日志级别')
    parser.add_argument('--date', help='日期 (YYYY-MM-DD), 默认今天')
    parser.add_argument('--keyword', help='搜索关键词')
    parser.add_argument('--tail', type=int, help='显示最后 N 行')
    parser.add_argument('--stats', action='store_true', help='显示统计信息')
    parser.add_argument('--report', action='store_true', help='生成时间段报告')
    parser.add_argument('--from', dest='start_date', help='开始日期 (用于报告)')
    parser.add_argument('--to', dest='end_date', help='结束日期 (用于报告)')
    parser.add_argument('--list', action='store_true', help='列出所有可用的日志日期')
    parser.add_argument('--log-dir', default='./logs', help='日志目录 (默认: ./logs)')
    
    args = parser.parse_args()
    
    analyzer = LogAnalyzer(args.log_dir)
    
    # 列出可用日期
    if args.list:
        dates = analyzer.get_available_dates()
        print(f"\n可用的日志日期 (共 {len(dates)} 天):\n")
        for date in dates[:30]:  # 最多显示30天
            print(f"  • {date}")
        if len(dates) > 30:
            print(f"\n  ... 还有 {len(dates) - 30} 个日期")
        print()
        return
    
    # 生成报告
    if args.report:
        if not args.start_date or not args.end_date:
            print(f"{Colors.RED}错误: 生成报告需要指定 --from 和 --to 参数{Colors.NC}")
            sys.exit(1)
        analyzer.generate_report(args.start_date, args.end_date)
        return
    
    # 获取日期
    date = args.date or datetime.now().strftime('%Y-%m-%d')
    
    # 显示统计
    if args.stats:
        stats = analyzer.generate_statistics(date)
        if not stats:
            print(f"{Colors.RED}错误: 未找到 {date} 的日志文件{Colors.NC}\n")
            print("可用的日志日期:")
            for d in analyzer.get_available_dates()[:10]:
                print(f"  • {d}")
            sys.exit(1)
        analyzer.print_statistics(stats, date)
        return
    
    # 过滤日志
    files = analyzer.get_log_files(date)
    if not files:
        print(f"{Colors.RED}错误: 未找到 {date} 的日志文件{Colors.NC}\n")
        print("可用的日志日期:")
        for d in analyzer.get_available_dates()[:10]:
            print(f"  • {d}")
        sys.exit(1)
    
    logs = analyzer.filter_logs(files, args.level, args.keyword)
    
    # 打印结果
    if not logs:
        print(f"{Colors.YELLOW}未找到匹配的日志{Colors.NC}")
        return
    
    print()
    analyzer.print_colored('=' * 60, Colors.CYAN)
    level_text = f"{args.level} 级别" if args.level else "所有级别"
    keyword_text = f" (关键词: {args.keyword})" if args.keyword else ""
    analyzer.print_colored(f'{level_text} 日志 - {date}{keyword_text}', Colors.BOLD)
    analyzer.print_colored('=' * 60, Colors.CYAN)
    print(f"\n找到 {len(logs)} 条日志\n")
    
    analyzer.print_logs(logs, args.tail)
    print()
    analyzer.print_colored('=' * 60, Colors.CYAN)
    
    # 提示
    if args.level == 'ERROR' and not args.stats:
        print()
        analyzer.print_colored('💡 提示:', Colors.YELLOW)
        print("  • 使用 --stats 查看完整统计")
        print(f"  • 查看警告: {sys.argv[0]} --level WARN --date {date}")
        print()

if __name__ == '__main__':
    main()
