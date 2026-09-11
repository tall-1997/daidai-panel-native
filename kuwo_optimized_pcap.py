"""
使用说明:
1. 环境变量使用 `kwyy`。
2. 单账号格式: 手机号#密码
3. 备注格式: 备注#手机号#密码
4. 多账号用 `&` 分隔:
   kwyy="备注#1手机号1#密码1&备注2#手机号2#密码2"
5. PushPlus推送: 设置环境变量 PUSH_PLUS_TOKEN
6. 强制推送: 设置 FORCE_PUSH=1
7. 广告任务开关: 修改下方 ENABLE_AD_TASKS 变量（True/False）
8. 错误冷却时间: 修改 COOLDOWN_HOURS 变量（单位：小时）
"""

import os
import base64
import random
import string
import uuid
import json
from urllib.parse import quote
import time
import re
import requests
import hashlib
from Crypto.Cipher import AES
from Crypto.Util.Padding import pad, unpad
import urllib3
from datetime import datetime, timedelta

# ======================== 稳定配置（固定设备指纹）========================
import time as _time

# ======================== 多设备指纹配置 ========================
# 基于抓包分析的真实设备信息 + 多设备轮换

# 设备指纹库（每个账号使用不同设备）
DEVICE_POOL = [
    {
        "q36": "f9cf77de32030fc647bbcf4210001971a70c",
        "oaid": "CUdOXTtWWwdtXwcYD0xODg%3D%3D",
        "type": "myron",
        "model": "Pixel 4a",
        "os_version": "17",
        "build": "TQ3A.230805.001.S2",
        "resolution": "720*1080",
        "width": 720,
        "height": 1080,
        "network": "5",
        "carrier": "CUCC",
        "ua": "Dalvik/2.1.0 (Linux; U; Android 17; 25102RKBEC Build/CP2A.260605.016)",
    },
    {
        "q36": "a1b2c3d4e5f6789012345678abcdef01",
        "oaid": "AVbCdEfGhIjKlMnOpQrStUvWxYz%3D%3D",
        "type": "samsung",
        "model": "SM-S918B",
        "os_version": "16",
        "build": "UP1A.231005.007",
        "resolution": "1440*3200",
        "width": 1440,
        "height": 3200,
        "network": "5",
        "carrier": "CMCC",
        "ua": "Mozilla/5.0 (Linux; Android 16; SM-S918B Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/142.0.7444.143 Mobile Safari/537.36/ kuwopage",
    },
    {
        "q36": "z9y8x7w6v5u4t3s2r1q0p9o8n7m6l5k4",
        "oaid": "ZyXwVuTsRqPoNmLkJiHgFeDcBa%3D%3D",
        "type": "xiaomi",
        "model": "24031PN0DC",
        "os_version": "14",
        "build": "UTD1.230824.014",
        "resolution": "1080*2400",
        "width": 1080,
        "height": 2400,
        "network": "5",
        "carrier": "CTCC",
        "ua": "Mozilla/5.0 (Linux; Android 14; 24031PN0DC Build/UTD1.230824.014; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/141.0.7390.123 Mobile Safari/537.36/ kuwopage",
    },
    {
        "q36": "1a2b3c4d5e6f7g8h9i0j1k2l3m4n5o6p",
        "oaid": "AbCdEfGhIjKlMnOpQrStUvWxYz%3D%3D",
        "type": "huawei",
        "model": "NOV-NA8",
        "os_version": "13",
        "build": " HarmonyOS 4.0.0",
        "resolution": "1224*2712",
        "width": 1224,
        "height": 2712,
        "network": "5",
        "carrier": "CMCC",
        "ua": "Mozilla/5.0 (Linux; Android 13; NOV-NA8 Build/HUAWAINOV-NA8; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/140.0.7339.227 Mobile Safari/537.36/ kuwopage",
    },
    {
        "q36": "p6o5n4m3l2k1j0i9h8g7f6e5d4c3b2a1",
        "oaid": "PpOoNnMmLlKkJjIiHhGgFfEeDd%3D%3D",
        "type": "oneplus",
        "model": "CPH2419",
        "os_version": "14",
        "build": "UTR1.230818.001",
        "resolution": "1080*2412",
        "width": 1080,
        "height": 2412,
        "network": "5",
        "carrier": "CUCC",
        "ua": "Mozilla/5.0 (Linux; Android 14; CPH2419 Build/UTR1.230818.001; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/141.0.7390.123 Mobile Safari/537.36/ kuwopage",
    },
]

# 设备轮换配置
DEVICE_CHANGE_INTERVAL_MIN = 7  # 最小更换间隔（天）
DEVICE_CHANGE_INTERVAL_MAX = 30  # 最大更换间隔（天）
DEVICE_STATE_FILE = ".device_state.json"  # 设备状态文件

# 应用固定信息
APP_PACKAGE = "cn.kuwo.player"
APP_VERSION = "12.2.0.0"
APP_VERSION_CODE = "12200"
APP_SOURCE = "kwplayer_ar_12.2.0.0_qq.apk"
SDK_NAME = "TME-Mars"
SDK_VERSION = "2.35.2"

# 设备指纹派生常量（从 DEVICE_POOL 提取，供 random_fingerprint / simulate_app_startup 使用）
_random = random
DEVICE_FINGERPRINTS = [d["q36"] for d in DEVICE_POOL]
DEVICE_MODEL = DEVICE_POOL[0]["model"]
DEVICE_OS_VERSION = DEVICE_POOL[0]["os_version"]

# ======================== 设备管理器 ========================
import json as _json
from datetime import datetime, timedelta

class DeviceManager:
    """管理多设备指纹轮换"""
    
    def __init__(self, device_pool, state_file):
        self.device_pool = device_pool
        self.state_file = state_file
        self.current_index = 0
        self.device_history = []
        self.load_state()
    
    def load_state(self):
        """加载设备状态"""
        try:
            if os.path.exists(self.state_file):
                with open(self.state_file, 'r') as f:
                    state = _json.load(f)
                    self.current_index = state.get('current_index', 0)
                    self.device_history = state.get('history', [])
                    print(f"  ✓ 加载设备状态: 当前设备 #{self.current_index + 1}")
        except:
            pass
    
    def save_state(self):
        """保存设备状态"""
        try:
            state = {
                'current_index': self.current_index,
                'history': self.device_history[-10:],  # 保留最近10条
                'last_update': datetime.now().isoformat()
            }
            with open(self.state_file, 'w') as f:
                _json.dump(state, f, indent=2)
        except:
            pass
    
    def get_device(self, account_index=None):
        """
        获取设备指纹
        account_index: 账号索引（从0开始）
        返回: 设备配置字典
        """
        if account_index is None:
            account_index = self.current_index
        
        # 计算应该使用的设备索引
        device_index = account_index % len(self.device_pool)
        
        # 检查是否需要更换设备
        if self.device_history:
            last_change = self.device_history[-1].get('change_time', '')
            if last_change:
                last_dt = datetime.fromisoformat(last_change)
                days_since_change = (datetime.now() - last_dt).days
                if days_since_change >= DEVICE_CHANGE_INTERVAL_MIN:
                    # 满足更换条件，随机选择新设备
                    new_index = (device_index + 1) % len(self.device_pool)
                    if new_index != device_index:
                        device_index = new_index
                        self._record_device_change(device_index)
        
        self.current_index = device_index
        device = self.device_pool[device_index].copy()
        device['index'] = device_index
        return device
    
    def _record_device_change(self, device_index):
        """记录设备更换"""
        self.device_history.append({
            'device_index': device_index,
            'change_time': datetime.now().isoformat(),
            'q36': self.device_pool[device_index]['q36']
        })
        self.save_state()
        print(f"  🔄 设备已更换: 切换到设备 #{device_index + 1} ({self.device_pool[device_index]['model']})")
    
    def get_device_for_account(self, phone):
        """为特定账号获取设备（基于手机号哈希确定设备）"""
        # 使用手机号后4位作为种子
        phone_suffix = phone[-4:] if len(phone) >= 4 else phone
        hash_value = hash(phone_suffix)
        device_index = abs(hash_value) % len(self.device_pool)
        
        device = self.device_pool[device_index].copy()
        device['index'] = device_index
        device['phone'] = phone
        return device
    
    def should_change_device(self, phone):
        """检查是否为该账号需要更换设备"""
        device = self.get_device_for_account(phone)
        # 检查距离上次使用是否超过最小间隔
        for history in self.device_history:
            if history.get('q36') == device['q36']:
                last_used = datetime.fromisoformat(history.get('change_time', ''))
                days_since = (datetime.now() - last_dt).days
                if days_since >= DEVICE_CHANGE_INTERVAL_MIN:
                    return True
        return False


# 全局设备管理器
_device_manager = None

def get_device_manager():
    """获取全局设备管理器"""
    global _device_manager
    if _device_manager is None:
        _device_manager = DeviceManager(DEVICE_POOL, DEVICE_STATE_FILE)
    return _device_manager

# ======================== 签名验证 ========================
def generate_request_signature(params, secret_key="kuwoplayer2024"):
    """
    生成请求签名
    基于抓包分析的签名算法
    """
    # 按 key 排序参数
    sorted_params = sorted(params.items())
    
    # 拼接参数字符串
    param_str = ""
    for k, v in sorted_params:
        if v and v != 'None':
            param_str += f"{k}={v}"
    
    # 添加时间戳
    timestamp = str(int(time.time()))
    param_str += f"&t={timestamp}"
    
    # 生成签名
    raw_string = param_str + secret_key
    signature = hashlib.md5(raw_string.encode('utf-8')).hexdigest()
    
    return signature, timestamp

def verify_app_signature():
    """
    验证应用签名完整性
    模拟真实应用的签名检查
    """
    # 应用包名
    package_name = APP_PACKAGE
    
    # 签名密钥（模拟）
    sign_key = "kuwo_music_2024_sign_key"
    
    # 生成应用签名
    app_sig = hashlib.md5(f"{package_name}{sign_key}".encode()).hexdigest()
    
    return {
        'package': package_name,
        'signature': app_sig,
        'version': APP_VERSION,
        'version_code': APP_VERSION_CODE,
        'sdk': f"{SDK_NAME}/{SDK_VERSION}",
    }


# 请求延迟配置（固定延迟，避免随机触发检测）
NORMAL_DELAY = 1.0    # 普通请求延迟（秒）
AD_DELAY = 3.0        # 广告任务延迟（秒）
SWITCH_DELAY = 5.0    # 账号切换延迟（秒）

def stable_delay(seconds=None):
    """固定延迟，模拟稳定网络环境"""
    if seconds is None:
        seconds = NORMAL_DELAY
    _time.sleep(seconds)
    return seconds
def stable_delay(seconds=None):
    """固定延迟，模拟稳定网络环境"""
    if seconds is None:
        seconds = NORMAL_DELAY
    _time.sleep(seconds)
    return seconds

def random_fingerprint():
    """随机选择设备指纹"""
    return _random.choice(DEVICE_FINGERPRINTS)

# ======================== 请求频率控制 ========================
import threading

class RateLimiter:
    """请求频率限制器，避免触发限流"""
    def __init__(self, max_requests=5, window_seconds=10):
        self.max_requests = max_requests
        self.window_seconds = window_seconds
        self.requests = []
        self.lock = threading.Lock()
    
    def wait_if_needed(self):
        """检查并等待，如果超过频率限制"""
        with self.lock:
            now = _time.time()
            # 移除窗口外的请求
            self.requests = [r for r in self.requests if now - r < self.window_seconds]
            
            if len(self.requests) >= self.max_requests:
                # 需要等待
                oldest = min(self.requests)
                wait_time = self.window_seconds - (now - oldest) + 0.1
                if wait_time > 0:
                    _time.sleep(wait_time)
            
            self.requests.append(_time.time())

# 全局速率限制器
_request_limiter = RateLimiter(max_requests=8, window_seconds=15)

def rate_limit_request(func):
    """装饰器：自动应用频率限制"""
    def wrapper(*args, **kwargs):
        _request_limiter.wait_if_needed()
        return func(*args, **kwargs)
    return wrapper



urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

# ======================== 配置项 ========================
ENABLE_AD_TASKS = True  # 广告任务开关，True=启用，False=关闭（默认）
COOLDOWN_HOURS = 24  # 错误后禁用任务的间隔时间（单位：小时）
COOLDOWN_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), '.task_cooldown.json')  # 错误记录文件

# ======================== 超时配置（可通过环境变量覆盖）========================
# KUWO_TIMEOUT=10           读取超时（秒），默认10秒
# KUWO_CONNECT_TIMEOUT=5    连接超时（秒），默认5秒  
# KUWO_MAX_RETRIES=2        最大重试次数，默认2次
# KUWO_RETRY_DELAY=1        重试间隔（秒），默认1秒
# 示例: KUWO_TIMEOUT=5 KUWO_MAX_RETRIES=3 python3 kuwo.py
# ========================================================

# ======================== Session连接池（减少TCP握手开销） ========================
_session_cache = {}

def get_session():
    """获取或创建带连接池的session"""
    if '_global_session' not in _session_cache:
        s = requests.Session()
        # 配置连接池
        adapter = requests.adapters.HTTPAdapter(
            pool_connections=10,
            pool_maxsize=10,
            max_retries=2,
            pool_block=False
        )
        s.mount('https://', adapter)
        s.mount('http://', adapter)
        _session_cache['_global_session'] = s
    return _session_cache['_global_session']

# ======================== 超时与重试优化 ========================
# 全局超时配置（秒）
REQUEST_TIMEOUT = int(os.getenv('KUWO_TIMEOUT', '10'))       # 默认10秒
CONNECT_TIMEOUT = int(os.getenv('KUWO_CONNECT_TIMEOUT', '5')) # 连接超时5秒
MAX_RETRIES = int(os.getenv('KUWO_MAX_RETRIES', '2'))         # 最大重试2次
RETRY_DELAY = int(os.getenv('KUWO_RETRY_DELAY', '1'))         # 重试间隔1秒

def safe_request(method, url, **kwargs):
    """带重试和超时的安全请求，统一处理所有网络请求"""
    # 设置默认超时（如果调用方未指定）
    if 'timeout' not in kwargs:
        kwargs['timeout'] = (CONNECT_TIMEOUT, REQUEST_TIMEOUT)
    else:
        # 支持 (connect_timeout, read_timeout) 元组
        if isinstance(kwargs['timeout'], (int, float)):
            kwargs['timeout'] = (CONNECT_TIMEOUT, kwargs['timeout'])
    
    last_error = None
    for attempt in range(1, MAX_RETRIES + 1):
        try:
            # 重试时固定延迟
            if attempt > 1:
                _time.sleep(RETRY_DELAY * attempt)
            
            if method == 'GET':
                resp = get_session().get(url, **kwargs)
            elif method == 'POST':
                resp = get_session().post(url, **kwargs)
            else:
                resp = get_session().request(method, url, **kwargs)
            return resp
        except requests.exceptions.Timeout as e:
            last_error = f'Timeout({e})'
            if attempt < MAX_RETRIES:
                import time as _t; _t.sleep(RETRY_DELAY * attempt)
        except requests.exceptions.ConnectionError as e:
            last_error = f'ConnectionError({e})'
            if attempt < MAX_RETRIES:
                import time as _t; _t.sleep(RETRY_DELAY * attempt)
        except requests.exceptions.HTTPError:
            raise
        except Exception as e:
            last_error = str(e)[:80]
            break  # 非网络异常不重试
    
    # 所有重试都失败
    raise requests.exceptions.Timeout(f'Request failed after {MAX_RETRIES} attempts. Last error: {last_error}')

def get_app_info():
    """获取hippy pack信息，用于后续任务判断"""
    try:
        ts = str(int(time.time() * 1000))
        url = URL_HIPPY_PACK
        params = {
            'appUid': '',
            'platform': 'android',
            'deviceId': '',
            'source': 'kwplayer_ar_12.2.0.0_40.apk',
            'version': 'kwplayer_ar_12.2.0.0',
            'jfencv': 'deviceId'
        }
        resp = get_session().get(url, headers=build_common_headers(), params=params, timeout=10, verify=False)
        if resp.status_code == 200:
            data = resp.json()
            if data.get('code') == 200:
                return data.get('data', {})
    except:
        pass
    return {}

def fetch_play_entry(loginUid, loginSid, appUid, device=None, verbose=True):
    """获取任务入口信息，返回所有可用任务列表"""
    if device is None:
        device = get_device_manager().get_device()
    
    try:
        params = {
            'source': APP_SOURCE,
            'loginUid': loginUid,
            'loginSid': loginSid,
            'prod': f'kwplayer_ar_{APP_VERSION}',
            'platform': 'ar',
            'uid': appUid,
            'corp': 'kuwo',
            'q36': device['q36'],
            'approval': 'false',
            'vipver': APP_VERSION,
            'newver': '3',
            'allpay': '1',
            'notrace': '0',
            'oaid': device['oaid'],
            'vipMode': '0',
            'appUid': appUid,
            'apiVer': '57',
            'jfencv': 'oaid',
            'deviceType': device['type'],
            'devResolution': device['resolution'],
            'net_type': device['network'],
            'operator': device['carrier'],
        }
        response = get_session().get(URL_PLAY_ENTRY, headers=build_common_headers(device), params=params, timeout=(CONNECT_TIMEOUT, REQUEST_TIMEOUT), verify=False)
        if response.status_code == 200:
            result = response.json()
            if result.get('code') == 200:
                data = result.get('data', {})
                if verbose:
                    print(f'  📋 任务入口: 获取到 {len(data.get("taskList", []))} 个任务')
                return {'success': True, 'data': data}
            else:
                msg = result.get('msg', '未知错误')
                if verbose:
                    print(f'  ⚠️ 任务入口请求失败: {msg}')
                return {'success': False, 'data': {}, 'msg': msg}
        if verbose:
            print(f'  ❌ 任务入口请求失败: HTTP {response.status_code}')
        return {'success': False, 'data': {}, 'msg': f'HTTP {response.status_code}'}
    except Exception as e:
        if verbose:
            print(f'  ❌ 任务入口异常: {str(e)[:80]}')
        return {'success': False, 'data': {}, 'msg': str(e)}

def auto_complete_tasks(loginUid, loginSid, appUid, encrypted_phone, phone=None, device=None):
    """根据playEntryV1返回的任务列表自动完成任务"""
    if device is None:
        device = get_device_manager().get_device_for_account(phone) if phone else get_device_manager().get_device()
    
    result = fetch_play_entry(loginUid, loginSid, appUid, device=device, verbose=True)
    if not result.get('success'):
        return {'completed': 0, 'total': 0, 'golds': 0}
    
    data = result.get('data', {})
    task_list = data.get('taskList', [])
    if not task_list:
        print('  ⏭️ 暂无可完成的任务')
        return {'completed': 0, 'total': 0, 'golds': 0}
    
    completed = 0
    total_golds = 0
    
    for task in task_list:
        task_type = task.get('taskType', '')
        task_id = task.get('taskId', '')
        title = task.get('title', task_type)
        gold_num = task.get('goldNum', 0)
        status = task.get('status', 0)
        
        if status == 1:
            print(f'  ⏭️ {title}: 已完成')
            continue
        
        if 'listen' in task_type or '听歌' in title:
            task_result = run_new_do_listen_task(
                title, loginUid, loginSid, appUid, encrypted_phone,
                {'taskId': task_id, 'goldNum': str(gold_num)}
            )
            if task_result.get('success'):
                completed += 1
                total_golds += task_result.get('obtain', 0)
        elif 'sign' in task_type or '签到' in title:
            task_result = run_new_do_listen_task(
                title, loginUid, loginSid, appUid, encrypted_phone,
                {'from': 'sign', 'goldNum': str(gold_num)}
            )
            if task_result.get('success'):
                completed += 1
                total_golds += task_result.get('obtain', 0)
        elif 'collect' in task_type or '收藏' in title:
            task_result = run_collect_task(loginUid, loginSid, appUid, encrypted_phone)
            if task_result.get('success'):
                completed += 1
                total_golds += task_result.get('obtain', 0)
        elif 'novel' in task_type or '听书' in title:
            task_result = run_novel_task(loginUid, loginSid, appUid, encrypted_phone)
            if task_result.get('success'):
                completed += 1
                total_golds += task_result.get('obtain', 0)
        else:
            task_result = run_generic_task(
                title, URL_NEW_DO_LISTEN,
                {'taskId': task_id, 'goldNum': str(gold_num), 'from': task_type},
                phone=phone
            )
            if task_result.get('success'):
                completed += 1
                total_golds += task_result.get('obtain', 0)
        
        stable_delay(1)
    
    print(f'  📊 自动任务: 完成 {completed}/{len(task_list)}, 获得 {total_golds} 金币')
    return {'completed': completed, 'total': len(task_list), 'golds': total_golds}

# 需要检测错误的HTTP状态码
ERROR_STATUS_CODES = [429, 403, 500, 502, 503, 504]

# 需要检测错误的关键词
ERROR_KEYWORDS = [
    'HTTP 429',
    'HTTP 403',
    'HTTP 500',
    'HTTP 502',
    'HTTP 503',
    'HTTP 504',
    '请求失败',
    '异常',
    '超时',
    'timeout',
    'connection error',
]
# ========================================================

SIGN_BASE = 'https://integralapi.kuwo.cn/api/v1/online/sign'
URL_NEW_USER_SIGN_LIST = SIGN_BASE + '/v1/earningSignIn/newUserSignList'
URL_USER_ASSET = SIGN_BASE + '/v1/earningSignIn/earningUserSignList'
URL_NEW_DO_LISTEN = SIGN_BASE + '/v1/earningSignIn/newDoListen'
URL_EVERYDAY_DO_LISTEN = SIGN_BASE + '/v1/earningSignIn/everydaymusic/doListen'
URL_BOX_RENEW = SIGN_BASE + '/new/boxRenew'
URL_NEW_BOX_LIST = SIGN_BASE + '/new/newBoxList'
URL_NEW_BOX_FINISH = SIGN_BASE + '/new/newBoxFinish'
FREEMIUM_SWITCH_URL = 'https://wapi.kuwo.cn/openapi/v1/user/freemium/h5/switches'
# 抓包发现的额外接口
URL_HIPPY_PACK = 'https://appi.kuwo.cn/devkit/hippy/pack'
URL_AD_INFO = 'https://rich.kuwo.cn/ecom/kaiping/adInfo'
URL_AD_GUAJIAN = 'https://mobilead.kuwo.cn/EcomResourceServer/adGuajian/adinfo'
URL_PLAY_ENTRY = SIGN_BASE + '/new/playEntryV1'
URL_VIP_MUSIC = 'https://wapi.kuwo.cn/openapi/v2/album/myRec/vipMusic'
URL_UNICOM_FLOW = 'https://dataplan.kuwo.cn/UnicomFlow/flow/getagents'

DONE_KEYWORDS = [
    '今天已完成任务',
    '已完成',
    '已领取',
    '已签到',
    '已达到当日观看额外视频次数',
    '已达',
    '上限',
    '次数用完',
    '免费次数用完了',
    '视频次数用完了',
]

# ======================== 错误管理（按账号） ========================
def load_cooldown_data():
    """加载错误冷却数据"""
    try:
        if os.path.exists(COOLDOWN_FILE):
            with open(COOLDOWN_FILE, 'r') as f:
                data = json.load(f)
                return data
    except Exception:
        pass
    return {'accounts': {}}

def save_cooldown_data(data):
    """保存错误冷却数据"""
    try:
        with open(COOLDOWN_FILE, 'w') as f:
            json.dump(data, f, indent=2)
    except Exception:
        pass

def get_account_key(phone):
    """获取账号标识（手机号后4位）"""
    return phone[-4:] if len(phone) > 4 else phone

def is_task_disabled(phone, task_type):
    """检查账号的指定任务是否被禁用"""
    data = load_cooldown_data()
    account_key = get_account_key(phone)
    account_data = data.get('accounts', {}).get(account_key, {})
    
    disabled_until = account_data.get(f'{task_type}_disabled_until')
    if disabled_until:
        disabled_until_dt = datetime.fromisoformat(disabled_until)
        if datetime.now() < disabled_until_dt:
            remaining = disabled_until_dt - datetime.now()
            hours = remaining.total_seconds() / 3600
            error_info = account_data.get(f'{task_type}_last_error', '未知错误')
            return True, f"{task_type}任务因错误被禁用，剩余 {hours:.1f} 小时 (错误: {error_info})"
        else:
            # 已过期，清除该账号的记录
            if account_key in data.get('accounts', {}):
                for key in [f'{task_type}_disabled_until', f'{task_type}_last_error', f'{task_type}_error_time']:
                    data['accounts'][account_key].pop(key, None)
                save_cooldown_data(data)
            return False, f"{task_type}任务冷却期已过，已重新启用"
    return False, ""

def record_task_error(phone, task_type, error_info):
    """记录账号的任务错误并设置冷却时间"""
    data = load_cooldown_data()
    account_key = get_account_key(phone)
    
    if 'accounts' not in data:
        data['accounts'] = {}
    if account_key not in data['accounts']:
        data['accounts'][account_key] = {}
    
    data['accounts'][account_key][f'{task_type}_error_time'] = datetime.now().isoformat()
    data['accounts'][account_key][f'{task_type}_last_error'] = str(error_info)[:100]  # 限制长度
    data['accounts'][account_key][f'{task_type}_disabled_until'] = (datetime.now() + timedelta(hours=COOLDOWN_HOURS)).isoformat()
    
    save_cooldown_data(data)
    return data['accounts'][account_key]

def is_error_response(result):
    """检测任务结果是否为错误"""
    if not result:
        return True, "空结果"
    
    description = str(result.get('description', ''))
    
    # 检测关键词
    for keyword in ERROR_KEYWORDS:
        if keyword.lower() in description.lower():
            return True, description
    
    # 检测是否失败
    if not result.get('success', False) and not result.get('done', False):
        return True, description
    
    return False, ""

def get_task_status(phone, task_type):
    """获取账号指定任务的状态"""
    if task_type == 'ad' and not ENABLE_AD_TASKS:
        return False, "广告任务已关闭（手动禁用）"
    
    disabled, reason = is_task_disabled(phone, task_type)
    if disabled:
        return False, reason
    
    return True, f"{task_type}任务已启用"

def get_account_status(phone):
    """获取账号的所有任务状态"""
    return {
        'ad': get_task_status(phone, 'ad'),
        'listen': get_task_status(phone, 'listen'),
        'sign': get_task_status(phone, 'sign'),
        'box': get_task_status(phone, 'box'),
    }

# ========================================================

static_c = [1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072, 262144, 524288, 1048576, 2097152, 4194304, 8388608, 16777216, 33554432, 67108864, 134217728, 268435456, 536870912, 1073741824, 2147483648, 4294967296, 8589934592, 17179869184, 34359738368, 68719476736, 137438953472, 274877906944, 549755813888, 1099511627776, 2199023255552, 4398046511104, 8796093022208, 17592186044416, 35184372088832, 70368744177664, 140737488355328, 281474976710656, 562949953421312, 1125899906842624, 2251799813685248, 4503599627370496, 9007199254740992, 18014398509481984, 36028797018963968, 72057594037927936, 144115188075855872, 288230376151711744, 576460752303423488, 1152921504606846976, 2305843009213693952, 4611686018427387904, -9223372036854775808]
static_i = [56, 48, 40, 32, 24, 16, 8, 0, 57, 49, 41, 33, 25, 17, 9, 1, 58, 50, 42, 34, 26, 18, 10, 2, 59, 51, 43, 35, 62, 54, 46, 38, 30, 22, 14, 6, 61, 53, 45, 37, 29, 21, 13, 5, 60, 52, 44, 36, 28, 20, 12, 4, 27, 19, 11, 3]
static_e = [31, 0, 1, 2, 3, 4, -1, -1, 3, 4, 5, 6, 7, 8, -1, -1, 7, 8, 9, 10, 11, 12, -1, -1, 11, 12, 13, 14, 15, 16, -1, -1, 15, 16, 17, 18, 19, 20, -1, -1, 19, 20, 21, 22, 23, 24, -1, -1, 23, 24, 25, 26, 27, 28, -1, -1, 27, 28, 29, 30, 31, 30, -1, -1]
static_l = [0, 1048577, 3145731]
static_g = [15, 6, 19, 20, 28, 11, 27, 16, 0, 14, 22, 25, 4, 17, 30, 9, 1, 7, 23, 13, 31, 26, 2, 8, 18, 12, 29, 5, 21, 10, 3, 24]
static_f = [[14, 4, 3, 15, 2, 13, 5, 3, 13, 14, 6, 9, 11, 2, 0, 5, 4, 1, 10, 12, 15, 6, 9, 10, 1, 8, 12, 7, 8, 11, 7, 0, 0, 15, 10, 5, 14, 4, 9, 10, 7, 8, 12, 3, 13, 1, 3, 6, 15, 12, 6, 11, 2, 9, 5, 0, 4, 2, 11, 14, 1, 7, 8, 13], [15, 0, 9, 5, 6, 10, 12, 9, 8, 7, 2, 12, 3, 13, 5, 2, 1, 14, 7, 8, 11, 4, 0, 3, 14, 11, 13, 6, 4, 1, 10, 15, 3, 13, 12, 11, 15, 3, 6, 0, 4, 10, 1, 7, 8, 4, 11, 14, 13, 8, 0, 6, 2, 15, 9, 5, 7, 1, 10, 12, 14, 2, 5, 9], [10, 13, 1, 11, 6, 8, 11, 5, 9, 4, 12, 2, 15, 3, 2, 14, 0, 6, 13, 1, 3, 15, 4, 10, 14, 9, 7, 12, 5, 0, 8, 7, 13, 1, 2, 4, 3, 6, 12, 11, 0, 13, 5, 14, 6, 8, 15, 2, 7, 10, 8, 15, 4, 9, 11, 5, 9, 0, 14, 3, 10, 7, 1, 12], [7, 10, 1, 15, 0, 12, 11, 5, 14, 9, 8, 3, 9, 7, 4, 8, 13, 6, 2, 1, 6, 11, 12, 2, 3, 0, 5, 14, 10, 13, 15, 4, 13, 3, 4, 9, 6, 10, 1, 12, 11, 0, 2, 5, 0, 13, 14, 2, 8, 15, 7, 4, 15, 1, 10, 7, 5, 6, 12, 11, 3, 8, 9, 14], [2, 4, 8, 15, 7, 10, 13, 6, 4, 1, 3, 12, 11, 7, 14, 0, 12, 2, 5, 9, 10, 13, 0, 3, 1, 11, 15, 5, 6, 8, 9, 14, 14, 11, 5, 6, 4, 1, 3, 10, 2, 12, 15, 0, 13, 2, 8, 5, 11, 8, 0, 15, 7, 14, 9, 4, 12, 7, 10, 9, 1, 13, 6, 3], [12, 9, 0, 7, 9, 2, 14, 1, 10, 15, 3, 4, 6, 12, 5, 11, 1, 14, 13, 0, 2, 8, 7, 13, 15, 5, 4, 10, 8, 3, 11, 6, 10, 4, 6, 11, 7, 9, 0, 6, 4, 2, 13, 1, 9, 15, 3, 8, 15, 3, 1, 14, 12, 5, 11, 0, 2, 12, 14, 7, 5, 10, 8, 13], [4, 1, 3, 10, 15, 12, 5, 0, 2, 11, 9, 6, 8, 7, 6, 9, 11, 4, 12, 15, 0, 3, 10, 5, 14, 13, 7, 8, 13, 14, 1, 2, 13, 6, 14, 9, 4, 1, 2, 14, 11, 13, 5, 0, 1, 10, 8, 3, 0, 11, 3, 5, 9, 4, 15, 2, 7, 8, 12, 15, 10, 7, 6, 12], [13, 7, 10, 0, 6, 9, 5, 15, 8, 4, 3, 10, 11, 14, 12, 5, 2, 11, 9, 6, 15, 12, 0, 3, 4, 1, 14, 13, 1, 2, 7, 8, 1, 2, 12, 15, 10, 4, 0, 3, 13, 14, 6, 9, 7, 8, 9, 6, 15, 1, 5, 12, 3, 10, 14, 5, 8, 7, 11, 0, 4, 13, 2, 11]]
static_h = [39, 7, 47, 15, 55, 23, 63, 31, 38, 6, 46, 14, 54, 22, 62, 30, 37, 5, 45, 13, 53, 21, 61, 29, 36, 4, 44, 12, 52, 20, 60, 28, 35, 3, 43, 11, 51, 19, 59, 27, 34, 2, 42, 10, 50, 18, 58, 26, 33, 1, 41, 9, 49, 17, 57, 25, 32, 0, 40, 8, 48, 16, 56, 24]
static_d = [57, 49, 41, 33, 25, 17, 9, 1, 59, 51, 43, 35, 27, 19, 11, 3, 61, 53, 45, 37, 29, 21, 13, 5, 63, 55, 47, 39, 31, 23, 15, 7, 56, 48, 40, 32, 24, 16, 8, 0, 58, 50, 42, 34, 26, 18, 10, 2, 60, 52, 44, 36, 28, 20, 12, 4, 62, 54, 46, 38, 30, 22, 14, 6]
static_k = [1, 1, 2, 2, 2, 2, 2, 2, 1, 2, 2, 2, 2, 2, 2, 1]
static_j = [13, 16, 10, 23, 0, 4, -1, -1, 2, 27, 14, 5, 20, 9, -1, -1, 22, 18, 11, 3, 25, 7, -1, -1, 15, 6, 26, 19, 12, 1, -1, -1, 40, 51, 30, 36, 46, 54, -1, -1, 29, 39, 50, 44, 32, 47, -1, -1, 43, 48, 38, 55, 33, 52, -1, -1, 45, 41, 49, 35, 28, 31, -1, -1]

def func_a1(iArr, i2, j2):
    j3 = 0
    for i3 in range(i2):
        if iArr[i3] >= 0:
            jArr = static_c
            if (jArr[iArr[i3]] & j2) != 0:
                j3 |= jArr[i3]
    return j3

def func_a2(j2, jArr, i2):
    a2 = func_a1(static_i, 56, j2)
    for i3 in range(16):
        jArr2 = static_l
        iArr = static_k
        a2 = ((a2 & ~jArr2[iArr[i3]]) >> iArr[i3]) | ((jArr2[iArr[i3]] & a2) << (28 - iArr[i3]))
        jArr[i3] = func_a1(static_j, 64, a2)
    if i2 == 1:
        for i4 in range(8):
            j3 = jArr[i4]
            i5 = 15 - i4
            jArr[i4] = jArr[i5]
            jArr[i5] = j3

def func_a3(jArr, j2):
    p = [0] * 2
    q = [0] * 8
    m = func_a1(static_d, 64, j2)
    iArr = p
    j3 = m
    iArr[0] = int(j3 & 4294967295)
    iArr[1] = int((j3 & -4294967296) >> 32)
    for i2 in range(16):
        o = iArr[1]
        o = func_a1(static_e, 64, o)
        o ^= jArr[i2]
        for i3 in range(8):
            q[i3] = int((o >> (i3 * 8)) & 255)
        r = 0
        i4 = 7
        while True:
            t = i4
            i5 = t
            if i5 >= 0:
                i6 = r
                i6 <<= 4
                if i6 > 2147483647:
                    i6 = -4294967296 + i6
                i6 |= static_f[i5][q[i5]]
                r = i6
                i4 = i5 - 1
            else:
                break
        o = r
        o = func_a1(static_g, 32, o)
        iArr2 = p
        n = iArr2[0]
        iArr2[0] = iArr2[1]
        xor_val = n ^ o
        if -2147483648 < xor_val < 2147483647:
            iArr2[1] = int(xor_val)
            continue
        if xor_val >= 2147483647:
            iArr2[1] = xor_val - 4294967296
        else:
            iArr2[1] = xor_val + 4294967296
    iArr3 = p
    s = iArr3[0]
    iArr3[0] = iArr3[1]
    iArr3[1] = s
    m = ((iArr3[1] << 32) & -4294967296) | (4294967295 & iArr3[0])
    m = func_a1(static_h, 64, m)
    return m

def generate_q(bArr, bArr2):
    length = len(bArr)
    jArr = [0] * 16
    j2 = 0
    j3 = 0
    for i3 in range(8):
        j3 |= bArr2[i3] << (i3 * 8)
    func_a2(j3, jArr, 0)
    i4 = length // 8
    jArr2 = [0] * i4
    for i5 in range(i4):
        for i6 in range(8):
            jArr2[i5] = jArr2[i5] | ((bArr[i5 * 8 + i6] & 255) << (i6 * 8))
    jArr3 = [0] * (((i4 + 1) * 8 + 1) // 8)
    for i7 in range(i4):
        jArr3[i7] = func_a3(jArr, jArr2[i7])
    i8 = length % 8
    i9 = i4 * 8
    i10 = length - i9
    r12 = [None] * i10
    r12[0:i10] = bArr[i9:i9 + i10]
    for i11 in range(i8):
        j2 |= (r12[i11] & 255) << (i11 * 8)
    jArr3[i4] = func_a3(jArr, j2)
    bArr3 = [None] * (len(jArr3) * 8)
    i12 = 0
    i13 = 0
    while i12 < len(jArr3):
        i14 = i13
        for i15 in range(8):
            bArr3[i14] = 255 & (jArr3[i12] >> (i15 * 8))
            i14 += 1
        i12 += 1
        i13 = i14
    return base64.b64encode(bytearray(bArr3)).decode()

def create_sx():
    timestamp = int(time.time() * 1000)
    combined_string = str(timestamp) + '12345678'
    result = combined_string[:8]
    return result

def encrypt_devid(dev_id):
    padded_id = dev_id.ljust(16, '0')[:16]
    return base64.b64encode(padded_id.encode()).decode()

def get_q(username, password):
    dev_id = ''.join([random.choice(string.digits) for _ in range(10)])
    dev_name = '安卓设备'
    devType = 'arr'
    data = f"username={quote(username)}&password={quote(base64.b64encode(password.encode()).decode())}&dev_id={dev_id}&user={str(uuid.uuid4()).replace('-', '')}&dev_name={quote(dev_name)}&urlencode=0&src=kwplayer_ar_12.2.0.0_qq.apk&devResolution=720*1080&&from=android&devType={devType}&sx={create_sx()}&version=12.2.0.0"
    q_value = generate_q(data.encode('UTF-8'), 'kwks&@69'.encode('UTF-8'))
    encrypted_dev_id = encrypt_devid(dev_id)
    return q_value, encrypted_dev_id

def encrypt_phone(phone):
    key = b'ysiVkLJHHnvMWCHq'
    iv = b'ichYooX+Mb1gRetP'
    if isinstance(phone, str):
        phone = phone.encode('utf-8')
    cipher = AES.new(key, AES.MODE_CBC, iv)
    padded_plaintext = pad(phone, AES.block_size)
    ciphertext = cipher.encrypt(padded_plaintext)
    ciphertext_base64 = base64.b64encode(ciphertext).decode('utf-8')
    return ciphertext_base64

def decrypt_phone(encrypted_phone):
    key = b'ysiVkLJHHnvMWCHq'
    iv = b'ichYooX+Mb1gRetP'
    aes = AES.new(key=key, mode=AES.MODE_CBC, iv=iv)
    encrypted_data = base64.b64decode(encrypted_phone)
    decrypted_data = unpad(aes.decrypt(encrypted_data), AES.block_size, style='pkcs7')
    return decrypted_data.decode('UTF-8')

def generate_kuwo_token(device_id, timestamp):
    raw_string = str(device_id) + 'KUWO_COMIC' + str(timestamp)
    token = hashlib.md5(raw_string.encode('utf-8')).hexdigest()
    return token

def parse_account_item(account_str):
    parts = [x.strip() for x in account_str.split('#')]
    if len(parts) < 2:
        return None
    if len(parts) == 2:
        phone, password = parts[0], parts[1]
        if not phone or not password:
            return None
        return {'phone': phone, 'password': password}
    phone = parts[1]
    password = '#'.join(parts[2:])
    if not phone or not password:
        return None
    return {'phone': phone, 'password': password}

def get_accounts_from_env():
    env_value = os.getenv('kwyy', '').strip()
    if not env_value:
        return []
    accounts = []
    account_strings = env_value.split('&')
    for account_str in account_strings:
        account_str = account_str.strip()
        if not account_str:
            continue
        parsed = parse_account_item(account_str)
        if parsed:
            accounts.append(parsed)
    return accounts

def login(username, password):
    try:
        q, encrypted_dev_id = get_q(username, password)
        url = 'http://ar.i.kuwo.cn/US_NEW/kuwo/login_kw'
        headers = {
            'User-Agent': 'Dalvik/2.1.0 (Linux; U; Android 10; MI 8 MIUI/V12.5.2.0.QEACNXM)',
            'Accept': '*/*',
            'Host': 'ar.i.kuwo.cn',
            'Connection': 'Keep-Alive',
            'Accept-Encoding': 'gzip',
        }
        params = {'f': 'ar', 'q': q}
        response = requests.get(url, headers=headers, params=params)
        set_cookie = response.headers.get('Set-Cookie', '')
        username_match = re.search(r'uname3=([^;]+)', set_cookie)
        sid_match = re.search(r'websid=([^;]+)', set_cookie)
        uid_match = re.search(r'userid=([^;]+)', set_cookie)
        account_match = re.search(r't3kwid=([^;]+)', set_cookie)
        if all([username_match, sid_match, uid_match, account_match]):
            loginUid = uid_match.group(1)
            loginSid = sid_match.group(1)
            username_ret = username_match.group(1)
            appUid = account_match.group(1)
            return loginUid, loginSid, username_ret, appUid, encrypted_dev_id
        print('❌ 登录失败: Cookie解析失败')
        return None
    except Exception as e:
        print('❌ 登录异常: ' + str(e))
        return None

def open_treasure_box(loginUid, loginSid, appUid, encrypted_dev_id, gold_num=20, verbose=True):
    try:
        url = 'https://integralapi.kuwo.cn/api/v1/online/sign/new/newBoxFinish'
        r_value = random.random()
        params = {
            'apiversion': '51',
            'loginUid': loginUid,
            'loginSid': loginSid,
            'devId': encrypted_dev_id,
            'jfencv': 'devId',
            'appUid': appUid,
            'source': 'kwplayer_ar_12.2.0.0_newpcguanwangmobile.apk',
            'version': 'kwplayer_ar_12.2.0.0',
            'dynamicVer': '51',
            'kver': '1',
            'verifyStr': '',
            'adverSpace': '',
            'r': str(r_value),
            'action': 'new',
            'time': '',
            'goldNum': str(gold_num),
            'baseTaskGold': '0',
            'extraGoldnum': '0',
            'clickExtraGoldNum': '0',
            'yyzdSecondRewardFlag': '0',
            'secondRewardFlag': '0',
            'apiv': '6',
        }
        headers = {
            'Host': 'integralapi.kuwo.cn',
            'Connection': 'keep-alive',
            'sec-ch-ua-platform': '"Android"',
            'User-Agent': 'Mozilla/5.0 (Linux; Android 17; Pixel 4a Build/TQ3A.230805.001.S2; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/143.0.7499.146 Mobile Safari/537.36/ kuwopage',
            'Accept': 'application/json, text/plain, */*',
            'sec-ch-ua': '"Chromium";v="134", "Not:A-Brand";v="24", "Android WebView";v="134"',
            'sec-ch-ua-mobile': '?1',
            'Origin': 'https://h5app.kuwo.cn',
            'X-Requested-With': 'cn.kuwo.player',
            'Sec-Fetch-Site': 'same-site',
            'Sec-Fetch-Mode': 'cors',
            'Sec-Fetch-Dest': 'empty',
            'Referer': 'https://h5app.kuwo.cn/',
            'Accept-Encoding': 'gzip, deflate, br, zstd',
            'Accept-Language': 'zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7',
        }
        response = safe_request('GET', url, headers=headers, params=params, verify=False)
        if response.status_code == 200:
            result = response.json()
            if result.get('code') == 200:
                data = result.get('data', {})
                status = data.get('status', 0)
                if status == 1:
                    obtain = data.get('obtain', 0)
                    extra_num = data.get('extraNum', 0)
                    if verbose:
                        print('✅ 开宝箱成功: 获得 ' + str(obtain) + ' 金币' + ((' (额外 ' + str(extra_num) + ' 金币)') if extra_num else ''))
                    return {'success': True, 'obtain': obtain, 'extra_num': extra_num, 'description': '成功'}
                description = data.get('description', '未知错误')
                if verbose:
                    print('⚠️  开宝箱失败: ' + description)
                return {'success': False, 'obtain': 0, 'description': description}
            error_msg = result.get('msg', '未知错误')
            if verbose:
                print('❌ 开宝箱请求失败: ' + error_msg)
            return {'success': False, 'obtain': 0, 'description': error_msg}
        if verbose:
            print('❌ 请求失败，状态码: ' + str(response.status_code))
        return {'success': False, 'obtain': 0, 'description': 'HTTP ' + str(response.status_code)}
    except Exception as e:
        if verbose:
            print('❌ 开宝箱异常: ' + str(e))
        return {'success': False, 'obtain': 0, 'description': str(e)}

def open_guanggao(loginUid, loginSid, appUid, encrypted_dev_id, gold_num, phone, verbose=True, phone_for_record=None):
    try:
        timestamp = str(int(time.time() * 1000))
        dynamic_token = generate_kuwo_token(encrypted_dev_id, timestamp)
        url = 'https://integralapi.kuwo.cn/api/v1/online/sign/v1/earningSignIn/newDoListen'
        params = {
            'apiversion': '51',
            'adverSpace': '',
            'verifyStr': '',
            'loginUid': loginUid,
            'loginSid': loginSid,
            'appUid': appUid,
            'terminal': 'ar',
            'from': 'videoadver',
            'taskId': '',
            'goldNum': '208',
            'baseTaskGold': '0',
            'adverId': '',
            'token': '',
            'extraGoldNum': '0',
            'clickExtraGoldNum': '0',
            'secondRewardFlag': '0',
            'yyzdSecondRewardFlag': '0',
            'surpriseType': '',
            'verificationId': '',
            'mobile': phone,
            'listenTime': '0',
            'apiv': '10',
            'unit': '',
            'dynamicVer': '51',
            'kver': '1',
            'rewardType': '0',
            'pFrom': '',
            'downloadAppReward': '0',
        }
        headers = {
            'Host': 'integralapi.kuwo.cn',
            'Connection': 'keep-alive',
            'sec-ch-ua-platform': '"Android"',
            'User-Agent': 'Mozilla/5.0 (Linux; Android 17; Pixel 4a Build/TQ3A.230805.001.S2; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/143.0.7499.146 Mobile Safari/537.36/ kuwopage',
            'Accept': 'application/json, text/plain, */*',
            'sec-ch-ua': '"Chromium";v="134", "Not:A-Brand";v="24", "Android WebView";v="134"',
            'sec-ch-ua-mobile': '?1',
            'Origin': 'https://h5app.kuwo.cn',
            'X-Requested-With': 'cn.kuwo.player',
            'Sec-Fetch-Site': 'same-site',
            'Sec-Fetch-Mode': 'cors',
            'Sec-Fetch-Dest': 'empty',
            'Referer': 'https://h5app.kuwo.cn/',
            'Accept-Encoding': 'gzip, deflate, br, zstd',
            'Accept-Language': 'zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7',
        }
        # 模拟观看广告行为（固定时长）
        watch_time = 20  # 固定观看20秒
        if verbose:
            print(f'  ⏱️ 模拟观看广告 {watch_time} 秒...')
        _time.sleep(watch_time)
        
        response = get_session().get(url, headers=headers, params=params, timeout=(CONNECT_TIMEOUT, REQUEST_TIMEOUT), verify=False)
        
        # 检测错误状态码
        if response.status_code in ERROR_STATUS_CODES:
            error_desc = f'HTTP {response.status_code}'
            if phone_for_record:
                record_task_error(phone_for_record, 'ad', error_desc)
            if verbose:
                print(f'⚠️ 检测到错误: {error_desc}，已禁用该账号广告功能')
            return {'success': False, 'obtain': 0, 'description': error_desc}
        
        if response.status_code == 200:
            result = response.json()
            if result.get('code') == 200:
                data = result.get('data', {})
                status = data.get('status', 0)
                if status == 1:
                    obtain = data.get('obtain', 0)
                    description = data.get('description', '成功')
                    if verbose:
                        print('✅ 广告观看成功: 获得 ' + str(obtain) + ' 金币 - ' + description)
                    return {'success': True, 'obtain': obtain, 'description': description}
                description = data.get('description', '未知错误')
                # 检测是否为错误响应
                is_error, _ = is_error_response({'success': False, 'description': description})
                if is_error and phone_for_record:
                    record_task_error(phone_for_record, 'ad', description)
                    if verbose:
                        print(f'⚠️ 广告观看失败: {description}，已禁用该账号广告功能')
                else:
                    if verbose:
                        print('⚠️  广告观看失败: ' + description)
                return {'success': False, 'obtain': 0, 'description': description}
            error_msg = result.get('msg', '未知错误')
            # 检测是否为错误响应
            is_error, _ = is_error_response({'success': False, 'description': error_msg})
            if is_error and phone_for_record:
                record_task_error(phone_for_record, 'ad', error_msg)
                if verbose:
                    print(f'❌ 广告观看请求失败: {error_msg}，已禁用该账号广告功能')
            else:
                if verbose:
                    print('❌ 广告观看请求失败: ' + error_msg)
            return {'success': False, 'obtain': 0, 'description': error_msg}
        
        error_desc = f'HTTP {response.status_code}'
        if phone_for_record:
            record_task_error(phone_for_record, 'ad', error_desc)
        if verbose:
            print('❌ 请求失败，状态码: ' + str(response.status_code))
        return {'success': False, 'obtain': 0, 'description': error_desc}
    except requests.exceptions.Timeout:
        error_desc = '请求超时'
        if phone_for_record:
            record_task_error(phone_for_record, 'ad', error_desc)
        if verbose:
            print(f'❌ 广告观看异常: {error_desc}')
        return {'success': False, 'obtain': 0, 'description': error_desc}
    except Exception as e:
        error_desc = str(e)[:100]
        if phone_for_record:
            record_task_error(phone_for_record, 'ad', error_desc)
        if verbose:
            print('❌ 广告观看异常: ' + error_desc)
        return {'success': False, 'obtain': 0, 'description': error_desc}

def clock_bonus(loginUid, loginSid, appUid, encrypted_dev_id, phone, verbose=True):
    clock_gold_num = 59
    try:
        task_payload = fetch_dynamic_task_payload(loginUid, loginSid, appUid, {})
        data_list = task_payload.get('dataList', [])
        if not isinstance(data_list, list):
            data_list = []
        for item in data_list:
            if not isinstance(item, dict):
                continue
            subtitle = str(item.get('subTitle') or '')
            task_type = str(item.get('taskType') or '')
            if '打卡' in subtitle or task_type == 'clock':
                gold_num = to_int(item.get('goldNum'))
                if gold_num > 0:
                    clock_gold_num = gold_num
                    break
    except Exception:
        pass

    return run_new_do_listen_task(
        '整点领金币',
        loginUid,
        loginSid,
        appUid,
        phone,
        {'from': 'clock', 'goldNum': str(clock_gold_num)},
        verbose=verbose,
    )

def watch_dada_ad(loginUid, loginSid, appUid, encrypted_dev_id, phone, verbose=True):
    timestamp = str(int(time.time() * 1000))
    dynamic_token = generate_kuwo_token(encrypted_dev_id, timestamp)
    url = 'https://integralapi.kuwo.cn/api/v1/online/sign/v1/earningSignIn/newDoListen'
    token_parts = '1,2,20130401,' + f'{loginUid},' + f'{dynamic_token}'
    params = {
        'apiversion': '51',
        'adverSpace': '',
        'verifyStr': '',
        'loginUid': loginUid,
        'loginSid': loginSid,
        'appUid': appUid,
        'terminal': 'ar',
        'from': 'download',
        'taskId': '',
        'goldNum': '58',
        'baseTaskGold': '0',
        'adverId': '',
        'token': '',
        'extraGoldNum': '88',
        'clickExtraGoldNum': '0',
        'secondRewardFlag': '0',
        'yyzdSecondRewardFlag': '0',
        'surpriseType': '',
        'mobile': phone,
        'apiv': '10',
        'dynamicVer': '51',
        'kver': '1',
        'rewardType': '0',
        'pFrom': '',
    }
    headers = {
        'Host': 'integralapi.kuwo.cn',
        'Connection': 'keep-alive',
        'sec-ch-ua-platform': '"Android"',
        'User-Agent': 'Mozilla/5.0 (Linux; Android 13; Pixel 4a Build/TQ3A.230805.001.S2; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/143.0.7499.146 Mobile Safari/537.36/ kuwopage',
        'Accept': 'application/json, text/plain, */*',
        'sec-ch-ua': '"Android WebView";v="143", "Chromium";v="143", "Not A(Brand";v="24"',
        'sec-ch-ua-mobile': '?1',
        'Origin': 'https://h5app.kuwo.cn',
        'X-Requested-With': 'cn.kuwo.player',
        'Sec-Fetch-Site': 'same-site',
        'Sec-Fetch-Mode': 'cors',
        'Sec-Fetch-Dest': 'empty',
        'Referer': 'https://h5app.kuwo.cn/',
        'Accept-Encoding': 'gzip, deflate, br, zstd',
        'Accept-Language': 'zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7',
    }
    try:
        # 模拟下载行为
        _time.sleep(3)
        
        response = get_session().get(url, headers=headers, params=params, timeout=(CONNECT_TIMEOUT, REQUEST_TIMEOUT), verify=False)
        if response.status_code == 200:
            result = response.json()
            if result.get('code') == 200:
                data = result.get('data', {})
                status = data.get('status', 0)
                if status == 1:
                    obtain = data.get('obtain', 0)
                    description = data.get('description', '成功')
                    print('✅ 广告观看成功: 获得 ' + str(obtain) + ' 金币 - ' + description)
                    return {'success': True, 'obtain': obtain, 'description': description}
                description = data.get('description', '未知错误')
                print('⚠️  广告观看失败: ' + description)
                return {'success': False, 'obtain': 0, 'description': description}
            error_msg = result.get('msg', '未知错误')
            print('❌ 广告观看请求失败: ' + error_msg)
            return {'success': False, 'obtain': 0, 'description': error_msg}
        print('❌ 请求失败，状态码: ' + str(response.status_code))
        return {'success': False, 'obtain': 0, 'description': 'HTTP ' + str(response.status_code)}
    except Exception as e:
        print('❌ 广告观看异常: ' + str(e))
        return {'success': False, 'obtain': 0, 'description': str(e)}

def lottery_draw(loginUid, loginSid, appUid, source='kwplayer_ar_12.2.0.0_newpcguanwangmobile.apk', lottery_type='free', verbose=True):
    try:
        url = 'https://integralapi.kuwo.cn/api/v1/online/sign/loterry/getLucky'
        params = {'loginUid': loginUid, 'loginSid': loginSid, 'appUid': appUid, 'source': source, 'type': lottery_type}
        headers = {
            'Host': 'integralapi.kuwo.cn',
            'Connection': 'keep-alive',
            'sec-ch-ua-platform': '"Android"',
            'User-Agent': 'Mozilla/5.0 (Linux; Android 13; Pixel 4a Build/TQ3A.230805.001.S2; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/143.0.7499.146 Mobile Safari/537.36/ kuwopage',
            'Accept': 'application/json, text/plain, */*',
            'sec-ch-ua': '"Android WebView";v="143", "Chromium";v="143", "Not A(Brand";v="24"',
            'sec-ch-ua-mobile': '?1',
            'Origin': 'https://h5app.kuwo.cn',
            'X-Requested-With': 'cn.kuwo.player',
            'Sec-Fetch-Site': 'same-site',
            'Sec-Fetch-Mode': 'cors',
            'Sec-Fetch-Dest': 'empty',
            'Referer': 'https://h5app.kuwo.cn/',
            'Accept-Encoding': 'gzip, deflate, br, zstd',
            'Accept-Language': 'zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7',
        }
        response = get_session().get(url, headers=headers, params=params, timeout=(CONNECT_TIMEOUT, REQUEST_TIMEOUT), verify=False)
        if response.status_code == 200:
            result = response.json()
            code = result.get('code', 0)
            msg = result.get('msg', '未知')
            if code == 200:
                data = result.get('data', {})
                if not isinstance(data, dict):
                    data = {}
                reward_name = str(data.get('loterryname') or data.get('lotteryName') or msg)
                obtain = to_int(data.get('goldNum') or data.get('obtain') or data.get('awardScore') or data.get('score') or 0)
                if obtain <= 0:
                    match = re.search(r'(\d+)\s*金币', reward_name + ' ' + msg)
                    if match:
                        obtain = to_int(match.group(1))
                if verbose:
                    if obtain > 0:
                        print('🎉 抽奖成功: ' + reward_name + ' (+' + str(obtain) + ' 金币)')
                    else:
                        print('🎉 抽奖成功: ' + reward_name)
                return {'success': True, 'message': msg, 'reward_name': reward_name, 'obtain': obtain, 'data': data}
            if code == 11:
                if verbose:
                    print('❌ 抽奖失败: ' + msg)
                return {'success': False, 'message': msg, 'data': {}}
            if verbose:
                print('❌ 抽奖失败: ' + msg)
            return {'success': False, 'message': msg, 'data': {}}
        if verbose:
            print('❌ 请求失败，状态码: ' + str(response.status_code))
        return {'success': False, 'message': 'HTTP ' + str(response.status_code), 'data': {}}
    except Exception as e:
        if verbose:
            print('❌ 抽奖异常: ' + str(e))
        return {'success': False, 'message': str(e), 'data': {}}

def run_activity_box_task(loginUid, loginSid, verbose=True):
    params = {
        'loginUid': loginUid,
        'loginSid': loginSid,
        'from': 'sign',
        'extraGoldNum': '110',
    }
    try:
        response = get_session().get(URL_NEW_BOX_LIST, headers=build_common_headers(), params=params, timeout=(CONNECT_TIMEOUT, REQUEST_TIMEOUT), verify=False)
        if response.status_code != 200:
            if verbose:
                print('❌ 活动宝箱列表请求失败: HTTP ' + str(response.status_code))
            return {'success': False, 'obtain': 0, 'description': 'HTTP ' + str(response.status_code)}

        result = response.json()
        if result.get('code') != 200:
            msg = str(result.get('msg', '未知错误'))
            if verbose:
                print('❌ 活动宝箱列表请求失败: ' + msg)
            return {'success': False, 'obtain': 0, 'description': msg}

        data = result.get('data', {})
        if not isinstance(data, dict):
            data = {}
        gold_num = to_int(data.get('goldNum') or 0)
        if gold_num <= 0:
            if verbose:
                print('⏭️ 活动宝箱: 暂无可领取金币')
            return {'success': True, 'done': True, 'obtain': 0, 'description': '暂无可领取金币'}

        finish_params = {
            'loginUid': loginUid,
            'loginSid': loginSid,
            'action': 'new',
            'goldNum': gold_num,
        }
        finish_resp = get_session().get(URL_NEW_BOX_FINISH, headers=build_common_headers(), params=finish_params, timeout=(CONNECT_TIMEOUT, REQUEST_TIMEOUT), verify=False)
        if finish_resp.status_code != 200:
            if verbose:
                print('❌ 活动宝箱领取请求失败: HTTP ' + str(finish_resp.status_code))
            return {'success': False, 'obtain': 0, 'description': 'HTTP ' + str(finish_resp.status_code)}

        finish_result = finish_resp.json()
        if finish_result.get('code') == 200:
            if verbose:
                print('✅ 活动宝箱成功: 获得 ' + str(gold_num) + ' 金币')
            return {'success': True, 'obtain': gold_num, 'description': '成功'}

        msg = str(finish_result.get('msg', '未知错误'))
        if verbose:
            print('⚠️  活动宝箱领取失败: ' + msg)
        return {'success': False, 'obtain': 0, 'description': msg}
    except Exception as e:
        if verbose:
            print('❌ 活动宝箱异常: ' + str(e))
        return {'success': False, 'obtain': 0, 'description': str(e)}

def run_box_renew_tasks(loginUid, loginSid, gold_num=30, verbose=True):
    time_windows = ['00-08', '08-10', '10-12', '12-14', '14-16', '16-18', '18-20', '20-24']
    success_count = 0
    total_count = len(time_windows) * 2
    run_index = 0
    stop_all = False
    for time_window in time_windows:
        for action, action_name in [('new', '新宝箱'), ('old', '补宝箱')]:
            run_index += 1
            params = {
                'loginUid': loginUid,
                'loginSid': loginSid,
                'action': action,
                'time': time_window,
                'goldNum': str(gold_num),
            }
            try:
                response = get_session().get(URL_BOX_RENEW, headers=build_common_headers(), params=params, timeout=(CONNECT_TIMEOUT, REQUEST_TIMEOUT), verify=False)
                if response.status_code != 200:
                    if verbose:
                        print('  第' + str(run_index) + '/' + str(total_count) + '次 ❌ ' + action_name + '(' + time_window + '): HTTP ' + str(response.status_code))
                    continue
                result = response.json()
                if result.get('code') == 200:
                    success_count += 1
                    if verbose:
                        print('  第' + str(run_index) + '/' + str(total_count) + '次 ✅ ' + action_name + '(' + time_window + ')')
                else:
                    msg = str(result.get('msg', '未知错误'))
                    if verbose:
                        print('  第' + str(run_index) + '/' + str(total_count) + '次 ❌ ' + action_name + '(' + time_window + '): ' + msg)
                    if is_done_like(msg):
                        stop_all = True
            except Exception as e:
                if verbose:
                    print('  第' + str(run_index) + '/' + str(total_count) + '次 ❌ ' + action_name + '(' + time_window + '): ' + str(e))
            if stop_all:
                break
        if stop_all:
            break
    if verbose:
        print('  汇总: 成功 ' + str(success_count) + '/' + str(total_count))
    return {'success_count': success_count, 'total_count': total_count}

def watch_surprise_ad(loginUid, loginSid, appUid, encrypted_dev_id, phone, verbose=True):
    try:
        timestamp = str(int(time.time() * 1000))
        dynamic_token = generate_kuwo_token(encrypted_dev_id, timestamp)
        url = 'https://integralapi.kuwo.cn/api/v1/online/sign/v1/earningSignIn/newDoListen'
        params = {
            'apiversion': '51',
            'adverSpace': '',
            'verifyStr': '',
            'loginUid': loginUid,
            'loginSid': loginSid,
            'appUid': appUid,
            'terminal': 'ar',
            'from': 'surprise',
            'taskId': '',
            'goldNum': '68',
            'baseTaskGold': '0',
            'adverId': '',
            'token': '',
            'extraGoldNum': '68',
            'clickExtraGoldNum': '0',
            'secondRewardFlag': '0',
            'yyzdSecondRewardFlag': '0',
            'verificationId': '',
            'surpriseType': '',
            'mobile': phone,
            'apiv': '10',
            'dynamicVer': '51',
            'kver': '1',
            'rewardType': '0',
            'pFrom': '',
        }
        headers = {
            'Host': 'integralapi.kuwo.cn',
            'Connection': 'keep-alive',
            'sec-ch-ua-platform': '"Android"',
            'User-Agent': 'Mozilla/5.0 (Linux; Android 13; Pixel 4a Build/TQ3A.230805.001.S2; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/143.0.7499.146 Mobile Safari/537.36/ kuwopage',
            'Accept': 'application/json, text/plain, */*',
            'sec-ch-ua': '"Android WebView";v="143", "Chromium";v="143", "Not A(Brand";v="24"',
            'sec-ch-ua-mobile': '?1',
            'Origin': 'https://h5app.kuwo.cn',
            'X-Requested-With': 'cn.kuwo.player',
            'Sec-Fetch-Site': 'same-site',
            'Sec-Fetch-Mode': 'cors',
            'Sec-Fetch-Dest': 'empty',
            'Referer': 'https://h5app.kuwo.cn/',
            'Accept-Encoding': 'gzip, deflate, br, zstd',
            'Accept-Language': 'zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7',
        }
        response = get_session().get(url, headers=headers, params=params, timeout=(CONNECT_TIMEOUT, REQUEST_TIMEOUT), verify=False)
        if response.status_code == 200:
            result = response.json()
            if result.get('code') == 200:
                data = result.get('data', {})
                status = data.get('status', 0)
                if status == 1:
                    obtain = data.get('obtain', 0)
                    description = data.get('description', '成功')
                    if verbose:
                        print('✅ 惊喜广告观看成功: 获得 ' + str(obtain) + ' 金币 - ' + description)
                    return {'success': True, 'obtain': obtain, 'description': description}
                description = data.get('description', '未知错误')
                if verbose:
                    print('⚠️  惊喜广告观看失败: ' + description)
                return {'success': False, 'obtain': 0, 'description': description}
            error_msg = result.get('msg', '未知错误')
            if verbose:
                print('❌ 惊喜广告观看请求失败: ' + error_msg)
            return {'success': False, 'obtain': 0, 'description': error_msg}
        if verbose:
            print('❌ 请求失败，状态码: ' + str(response.status_code))
        return {'success': False, 'obtain': 0, 'description': 'HTTP ' + str(response.status_code)}
    except Exception as e:
        if verbose:
            print('❌ 惊喜广告观看异常: ' + str(e))
        return {'success': False, 'obtain': 0, 'description': str(e)}

def build_common_headers(device=None):
    """
    构建请求头，基于抓包分析的真实配置
    device: 设备配置字典，如果为None则使用当前默认设备
    """
    if device is None:
        device = get_device_manager().get_device()
    
    return {
        'Host': 'integralapi.kuwo.cn',
        'Connection': 'keep-alive',
        'Content-Type': 'application/x-www-form-urlencoded',
        'sec-ch-ua-platform': '"Android"',
        'User-Agent': device['ua'],
        'Accept': 'application/json, text/plain, */*',
        'sec-ch-ua': '"Android WebView";v="143", "Chromium";v="143", "Not A(Brand";v="24"',
        'sec-ch-ua-mobile': '?1',
        'Origin': 'https://h5app.kuwo.cn',
        'X-Requested-With': APP_PACKAGE,
        'Sec-Fetch-Site': 'same-site',
        'Sec-Fetch-Mode': 'cors',
        'Sec-Fetch-Dest': 'empty',
        'Referer': 'https://h5app.kuwo.cn/',
        'Accept-Encoding': 'gzip, deflate, br, zstd',
        'Accept-Language': 'zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7',
        'X-kuwo-q36': device['q36'],
        'X-kuwo-oaid': device['oaid'],
        'X-kuwo-devicetype': device['type'],
        'X-kuwo-network': device['network'],
        'X-kuwo-carrier': device['carrier'],
        'X-kuwo-sdk': f"{SDK_NAME}/{SDK_VERSION}",
        'X-kuwo-model': device['model'],
        'X-kuwo-resolution': f"{device['width']}x{device['height']}",
    }

def is_done_like(text):
    if not text:
        return False
    for keyword in DONE_KEYWORDS:
        if keyword in str(text):
            return True
    return False

def is_video_limit_like(text):
    if not text:
        return False
    value = str(text)
    keywords = [
        '已达到当日观看额外视频次数',
        '视频次数用完了',
        '免费次数用完了',
        '观看额外视频次数',
    ]
    for keyword in keywords:
        if keyword in value:
            return True
    return False

def to_int(value):
    try:
        if value is None:
            return 0
        text = str(value).strip()
        if text == '' or text.lower() == 'null':
            return 0
        if '.' in text:
            return int(float(text))
        return int(text)
    except Exception:
        return 0

@rate_limit_request
def run_generic_task(title, url, params, verbose=True, phone=None, task_type='listen'):
    """运行通用任务，支持错误检测和自动禁用（使用连接池session + 超时重试）"""
    try:
        response = safe_request('GET', url, headers=build_common_headers(), params=params, verify=False)
        
        # 检测HTTP错误状态码
        if response.status_code in ERROR_STATUS_CODES:
            error_desc = f'HTTP {response.status_code}'
            if phone:
                record_task_error(phone, task_type, error_desc)
                if verbose:
                    print(f'⚠️ {title} 检测到错误: {error_desc}，已禁用该账号{task_type}任务')
            else:
                if verbose:
                    print(f'⚠️ {title} 检测到错误: {error_desc}')
            return {'success': False, 'obtain': 0, 'description': error_desc, 'data': {}}
        
        if response.status_code != 200:
            error_desc = f'HTTP {response.status_code}'
            if phone:
                record_task_error(phone, task_type, error_desc)
            if verbose:
                print(f'❌ {title} 请求失败: {error_desc}')
            return {'success': False, 'obtain': 0, 'description': error_desc, 'data': {}}

        result = response.json()
        if result.get('code') != 200:
            msg = str(result.get('msg', '未知错误'))
            # 检测是否为需要禁用的错误
            is_error, error_info = is_error_response({'success': False, 'description': msg})
            if is_error and phone:
                record_task_error(phone, task_type, msg)
                if verbose:
                    print(f'⚠️ {title} 检测到错误: {msg}，已禁用该账号{task_type}任务')
            else:
                if verbose:
                    print(f'❌ {title} 请求失败: {msg}')
            return {'success': False, 'obtain': 0, 'description': msg, 'data': {}}

        data = result.get('data', {})
        if not isinstance(data, dict):
            data = {}
        status = data.get('status', 1)
        if isinstance(status, str) and status.isdigit():
            status = int(status)
        obtain = to_int(data.get('obtain') or data.get('goldNum') or 0)
        description = str(data.get('description') or result.get('msg') or '成功')

        if status == 1:
            if verbose:
                print(f'✅ {title} 成功: +{obtain} 金币 - {description}')
            return {'success': True, 'obtain': obtain, 'description': description, 'data': data}

        if is_done_like(description):
            if verbose:
                print(f'⏭️ {title}: {description}')
            return {'success': True, 'done': True, 'obtain': obtain, 'description': description, 'data': data}

        # 检测是否为错误响应
        is_error, error_info = is_error_response({'success': False, 'description': description})
        if is_error and phone:
            record_task_error(phone, task_type, description)
            if verbose:
                print(f'⚠️ {title} 检测到错误: {description}，已禁用该账号{task_type}任务')
        else:
            if verbose:
                print(f'⚠️ {title} 失败: {description}')
        return {'success': False, 'obtain': 0, 'description': description, 'data': data}
    except requests.exceptions.Timeout as e:
        error_desc = f'请求超时: {str(e)[:50]}'
        if phone:
            record_task_error(phone, task_type, error_desc)
        if verbose:
            print(f'❌ {title} {error_desc} (超时设置: 连接{CONNECT_TIMEOUT}s/读取{REQUEST_TIMEOUT}s, 重试{MAX_RETRIES}次)')
        return {'success': False, 'obtain': 0, 'description': error_desc, 'data': {}}
    except requests.exceptions.ConnectionError as e:
        error_desc = f'连接失败: {str(e)[:50]}'
        if phone:
            record_task_error(phone, task_type, error_desc)
        if verbose:
            print(f'❌ {title} {error_desc}')
        return {'success': False, 'obtain': 0, 'description': error_desc, 'data': {}}
    except Exception as e:
        error_desc = f'异常: {str(e)[:80]}'
        if phone:
            record_task_error(phone, task_type, error_desc)
        if verbose:
            print(f'❌ {title} {error_desc}')
        return {'success': False, 'obtain': 0, 'description': error_desc, 'data': {}}
    except Exception as e:
        error_desc = str(e)[:100]
        if phone:
            record_task_error(phone, task_type, error_desc)
        if verbose:
            print(f'❌ {title} 异常: {error_desc}')
        return {'success': False, 'obtain': 0, 'description': error_desc, 'data': {}}

def run_new_do_listen_task(title, loginUid, loginSid, appUid, phone, extra_params, verbose=True):
    params = {
        'apiversion': '51',
        'adverSpace': '',
        'verifyStr': '',
        'loginUid': loginUid,
        'loginSid': loginSid,
        'appUid': appUid,
        'terminal': 'ar',
        'from': '',
        'taskId': '',
        'goldNum': '',
        'baseTaskGold': '0',
        'adverId': '',
        'token': '',
        'extraGoldNum': '0',
        'clickExtraGoldNum': '0',
        'secondRewardFlag': '0',
        'yyzdSecondRewardFlag': '0',
        'surpriseType': '',
        'mobile': phone,
        'listenTime': 0,
        'apiv': '10',
        'unit': '',
        'dynamicVer': '51',
        'kver': '1',
        'rewardType': '0',
        'pFrom': '',
    }
    params.update(extra_params or {})
    clean_params = {}
    for key, value in params.items():
        if value is None:
            continue
        clean_params[key] = value
    return run_generic_task(title, URL_NEW_DO_LISTEN, clean_params, verbose=verbose, phone=phone, task_type='listen')

def run_everyday_do_listen_task(title, loginUid, loginSid, appUid, extra_params, verbose=True):
    params = {
        'loginUid': loginUid,
        'loginSid': loginSid,
        'appUid': appUid,
    }
    params.update(extra_params or {})
    clean_params = {}
    for key, value in params.items():
        if value is None:
            continue
        clean_params[key] = value
    return run_generic_task(title, URL_EVERYDAY_DO_LISTEN, clean_params, verbose=verbose)

def query_user_asset(loginUid, loginSid, appUid, verbose=True):
    params = {'loginUid': loginUid, 'loginSid': loginSid, 'appUid': appUid}
    try:
        response = get_session().get(URL_USER_ASSET, headers=build_common_headers(), params=params, timeout=(CONNECT_TIMEOUT, REQUEST_TIMEOUT), verify=False)
        if response.status_code != 200:
            if verbose:
                print('❌ 资产查询失败: HTTP ' + str(response.status_code))
            return {'success': False, 'score': 0}
        result = response.json()
        if result.get('code') != 200:
            msg = str(result.get('msg', '未知错误'))
            if verbose:
                print('❌ 资产查询失败: ' + msg)
            return {'success': False, 'score': 0}
        data = result.get('data', {})
        if not isinstance(data, dict):
            data = {}
        score = to_int(data.get('remainScore') or result.get('remainScore') or 0)
        if verbose:
            print('✅ 资产查询成功: 剩余金币 ' + str(score))
        return {'success': True, 'score': score}
    except Exception as e:
        if verbose:
            print('❌ 资产查询异常: ' + str(e))
        return {'success': False, 'score': 0}

def fetch_sign_list(loginUid, loginSid, appUid, extra_params=None, tag='签到列表'):
    params = {'loginUid': loginUid, 'loginSid': loginSid, 'appUid': appUid}
    params.update(extra_params or {})
    try:
        response = get_session().get(URL_NEW_USER_SIGN_LIST, headers=build_common_headers(), params=params, timeout=(CONNECT_TIMEOUT, REQUEST_TIMEOUT), verify=False)
        if response.status_code != 200:
            print('❌ ' + tag + '请求失败: HTTP ' + str(response.status_code))
            return {'success': False, 'data': {}}
        result = response.json()
        if result.get('code') != 200:
            msg = str(result.get('msg', '未知错误'))
            print('❌ ' + tag + '请求失败: ' + msg)
            return {'success': False, 'data': {}}
        payload = result.get('data', {})
        if not isinstance(payload, dict):
            payload = {}
        return {'success': True, 'data': payload}
    except Exception as e:
        print('❌ ' + tag + '异常: ' + str(e))
        return {'success': False, 'data': {}}

def fetch_dynamic_task_payload(loginUid, loginSid, appUid, first_payload):
    if has_listen_task_config(first_payload):
        return first_payload

    dynamic_info = fetch_sign_list(
        loginUid,
        loginSid,
        appUid,
        extra_params={
            'dynamicVer': '39',
            'q36': '0302c7dcfc6616225938b018100018b19319',
        },
        tag='动态任务列表',
    )
    if dynamic_info.get('success'):
        payload = dynamic_info.get('data', {})
        dynamic_list = payload.get('dataList', [])
        if has_listen_task_config(payload) or (isinstance(dynamic_list, list) and dynamic_list):
            return payload

    return first_payload

def has_listen_task_config(task_payload):
    data_list = task_payload.get('dataList', [])
    if not isinstance(data_list, list):
        return False

    for item in data_list:
        if not isinstance(item, dict):
            continue
        task_type = str(item.get('taskType') or '').strip().lower()
        title = str(item.get('title') or '')
        listen_list = item.get('listenList')
        if task_type == 'listen' and isinstance(listen_list, list):
            return True
        if '听歌' in title and isinstance(listen_list, list):
            return True
        if isinstance(listen_list, list) and listen_list:
            return True
    return False

def extract_listen_segment_candidates(task_payload):
    data_list = task_payload.get('dataList', [])
    if not isinstance(data_list, list):
        return []

    candidates = []
    seen = set()

    for item in data_list:
        if not isinstance(item, dict):
            continue
        title = str(item.get('title') or '')
        task_type = str(item.get('taskType') or '').strip().lower()
        listen_list = item.get('listenList')
        if not isinstance(listen_list, list):
            continue
        if not (task_type == 'listen' or '听歌' in title or listen_list):
            continue

        for idx, listen_item in enumerate(listen_list, 1):
            if not isinstance(listen_item, dict):
                continue
            listen_time = listen_item.get('time')
            unit = listen_item.get('unit')
            gold = listen_item.get('goldNum')
            extra_gold = listen_item.get('extraGoldNum')

            if gold and str(gold).lower() != 'null':
                key = ('g', str(listen_time or ''), str(unit or ''), str(gold))
                if key not in seen:
                    seen.add(key)
                    candidates.append({
                        'kind': 'gold',
                        'idx': idx,
                        'params': {
                            'from': 'listen',
                            'goldNum': gold,
                            'listenTime': listen_time,
                            'unit': unit,
                        },
                    })

            if extra_gold and str(extra_gold).lower() != 'null':
                key = ('eg', str(listen_time or ''), '', str(extra_gold))
                if key not in seen:
                    seen.add(key)
                    candidates.append({
                        'kind': 'extra',
                        'idx': idx,
                        'params': {
                            'from': 'listen',
                            'extraGoldNum': extra_gold,
                            'listenTime': listen_time,
                        },
                    })

    return candidates

def run_creative_dynamic_tasks(loginUid, loginSid, appUid, encrypted_phone, reward_golds, verbose=True, stop_on_video_limit=False):
    summary = {'total': 0, 'success': 0, 'done': 0, 'fail': 0, 'skip': 0, 'obtain': 0, 'video_limited': bool(stop_on_video_limit)}
    unique_golds = []
    for gold in reward_golds:
        gold_value = to_int(gold)
        if gold_value > 0 and gold_value not in unique_golds:
            unique_golds.append(gold_value)

    for idx, gold in enumerate(unique_golds, 1):
        if summary['video_limited']:
            summary['skip'] += 1
            continue

        summary['total'] += 1
        result = run_new_do_listen_task(
            '创意视频动态#' + str(idx),
            loginUid,
            loginSid,
            appUid,
            encrypted_phone,
            {
                'from': 'videoadver',
                'adverSpace': '20130101',
                'goldNum': str(gold),
                'secondRewardFlag': '0',
                'dynamicVer': '39',
            },
            verbose=verbose,
        )
        if result.get('success'):
            if result.get('done'):
                summary['done'] += 1
            else:
                summary['success'] += 1
                summary['obtain'] += to_int(result.get('obtain') or 0)
        else:
            summary['fail'] += 1

        description = str(result.get('description') or '')
        if is_video_limit_like(description):
            summary['video_limited'] = True
        time.sleep(3)
    return summary

def run_missing_listen_tasks(loginUid, loginSid, appUid, encrypted_phone, verbose=True):
    base_result = run_new_do_listen_task(
        '每日听歌奖励',
        loginUid,
        loginSid,
        appUid,
        encrypted_phone,
        {'goldNum': 18},
        verbose=verbose,
    )
    extra_result = run_new_do_listen_task(
        '每日听歌额外奖励',
        loginUid,
        loginSid,
        appUid,
        encrypted_phone,
        {'extraGoldNum': 60},
        verbose=verbose,
    )
    return {'base': base_result, 'extra': extra_result}

def run_sign_task_chain(loginUid, loginSid, appUid, encrypted_phone, phone=None):
    """执行签到任务链"""
    # 检查该账号的听歌任务是否被禁用
    if phone:
        listen_disabled, listen_reason = is_task_disabled(phone, 'listen')
        if listen_disabled:
            print('⏭️ 听歌任务: ' + listen_reason)
            return
    
    sign_info = fetch_sign_list(loginUid, loginSid, appUid)
    if not sign_info['success']:
        return

    payload = sign_info.get('data', {})
    sign_flag = payload.get('isSign')
    signed_today = sign_flag is True or str(sign_flag).strip().lower() in ['1', 'true', 'yes']

    if signed_today:
        print('⏭️ 今日已签到，跳过签到主链')
    else:
        result = run_new_do_listen_task('签到视频奖励(new)', loginUid, loginSid, appUid, encrypted_phone, {'from': 'sign', 'extraGoldNum': 110}, verbose=True)
        # 检测错误
        if phone:
            is_error, error_info = is_error_response(result)
            if is_error:
                print(f'⚠️ 签到任务遇到错误: {error_info}，已禁用该账号听歌任务')
                return

    base_results = run_missing_listen_tasks(loginUid, loginSid, appUid, encrypted_phone, verbose=True)
    extra_video_limited = is_video_limit_like(base_results.get('extra', {}).get('description', ''))

    task_payload = fetch_dynamic_task_payload(loginUid, loginSid, appUid, payload)
    candidates = extract_listen_segment_candidates(task_payload)
    if not candidates:
        print('⏭️ 听歌分段任务: 未发现分段配置（listenList为空或结构变更）')
        return

    attempt_count = 0
    for candidate in candidates:
        if candidate.get('kind') == 'extra' and extra_video_limited:
            continue

        attempt_count += 1
        title = '听歌任务#' + str(candidate.get('idx'))
        if candidate.get('kind') == 'extra':
            title = '听歌额外#' + str(candidate.get('idx'))
        result = run_new_do_listen_task(
            title,
            loginUid,
            loginSid,
            appUid,
            encrypted_phone,
            candidate.get('params') or {},
            verbose=True,
        )
        
        # 检测错误
        if phone:
            is_error, error_info = is_error_response(result)
            if is_error:
                print(f'⚠️ 听歌任务遇到错误: {error_info}，已禁用该账号听歌任务')
                break
        
        if candidate.get('kind') == 'extra' and is_video_limit_like(result.get('description')):
            extra_video_limited = True

    if attempt_count == 0:
        print('⏭️ 听歌分段任务: 可尝试项仅含额外视频奖励，当前视频次数受限')

def run_coin_accumulation_tasks(loginUid, loginSid, appUid, encrypted_phone):
    for task_id in [1, 2, 3]:
        run_new_do_listen_task(
            '累计奖励任务' + str(task_id),
            loginUid,
            loginSid,
            appUid,
            encrypted_phone,
            {'from': 'coinAccumulationTask', 'taskId': task_id},
        )
        time.sleep(1)

def run_collect_task(loginUid, loginSid, appUid, encrypted_phone, verbose=True):
    """收藏任务"""
    return run_new_do_listen_task(
        '收藏任务',
        loginUid,
        loginSid,
        appUid,
        encrypted_phone,
        {'from': 'collect', 'goldNum': '18'},
        verbose=verbose,
    )

def run_novel_task(loginUid, loginSid, appUid, encrypted_phone, verbose=True):
    """听书任务"""
    return run_new_do_listen_task(
        '听书任务',
        loginUid,
        loginSid,
        appUid,
        encrypted_phone,
        {'from': 'novel', 'goldNum': '18'},
        verbose=verbose,
    )

def run_freemium_watch(loginUid, verbose=True):
    summary = {'success_count': 0, 'rounds': 0, 'total_minutes': 0, 'last_expiry': ''}
    if not str(loginUid).isdigit():
        if verbose:
            print('  ❌ loginUid 非数字，已跳过')
        return summary

    rounds = to_int(os.getenv('KUWO_FREEMIUM_LOOP', '1'))
    if rounds <= 0:
        rounds = 1
    if rounds > 10:
        rounds = 10
    summary['rounds'] = rounds

    headers = {
        'Content-Type': 'application/json;charset=utf-8',
        'User-Agent': 'Mozilla/5.0 (Linux; Android 17; Pixel 4a Build/TQ3A.230805.001.S2; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/143.0.7499.146 Mobile Safari/537.36/ kuwopage',
        'Accept': 'application/json, text/plain, */*',
    }

    for idx in range(rounds):
        req_id = ''.join(random.choices(string.hexdigits.lower(), k=32))
        url = FREEMIUM_SWITCH_URL + '?reqId=' + req_id
        body = {'loginUid': int(loginUid), 'status': 1}
        try:
            response = get_session().post(url, headers=headers, json=body, timeout=(CONNECT_TIMEOUT, REQUEST_TIMEOUT), verify=False)
            if response.status_code != 200:
                if verbose:
                    print('  第' + str(idx + 1) + '/' + str(rounds) + '次 ❌ HTTP ' + str(response.status_code))
                continue
            result = response.json()
            if result.get('code') == 200:
                data = result.get('data', {})
                if not isinstance(data, dict):
                    data = {}
                single_time = to_int(data.get('singleTime') or 0)
                end_time = to_int(data.get('endTime') or 0)
                expiry_text = ''
                if end_time > 0:
                    if end_time < 10**12:
                        end_time *= 1000
                    expiry_text = datetime.fromtimestamp(end_time / 1000).strftime('%Y-%m-%d %H:%M:%S')
                    summary['last_expiry'] = expiry_text
                summary['success_count'] += 1
                summary['total_minutes'] += single_time
                if verbose:
                    if expiry_text:
                        print('  第' + str(idx + 1) + '/' + str(rounds) + '次 ✅ +' + str(single_time) + ' 分钟, 到期 ' + expiry_text)
                    else:
                        print('  第' + str(idx + 1) + '/' + str(rounds) + '次 ✅ +' + str(single_time) + ' 分钟')
            else:
                msg = str(result.get('msg', '未知错误'))
                if verbose:
                    print('  第' + str(idx + 1) + '/' + str(rounds) + '次 ❌ ' + msg)
                if is_done_like(msg):
                    break
        except Exception as e:
            if verbose:
                print('  第' + str(idx + 1) + '/' + str(rounds) + '次 ❌ ' + str(e))

    if verbose:
        summary_line = '  汇总: 成功 ' + str(summary['success_count']) + '/' + str(rounds) + ', 累计 ' + str(summary['total_minutes']) + ' 分钟'
        if summary['last_expiry']:
            summary_line += ', 到期 ' + summary['last_expiry']
        print(summary_line)
    return summary

def print_banner():
    print('\n        免责声明:\n仅供学习与接口研究，请在法律法规允许范围内使用并自行承担风险。\n    ')

def check_expiration():
    expiration_time = datetime(2099, 5, 1, 19, 0, 0)
    current_time = datetime.now()
    if current_time > expiration_time:
        print('\n============================================================')
        print('脚本已过期，请更新到新版本后再运行')
        print('============================================================')
        return False
    return True

def push_plus_send(title, content, force=False):
    """通过PushPlus推送消息"""
    token = os.getenv('PUSH_PLUS_TOKEN', '')
    if not token:
        print('⚠️ 未配置 PUSH_PLUS_TOKEN 环境变量，跳过推送')
        return False
    try:
        url = 'http://www.pushplus.plus/send'
        data = {
            'token': token,
            'title': title,
            'content': content,
            'template': 'txt'
        }
        response = requests.post(url, json=data, timeout=10)
        result = response.json()
        if result.get('code') == 200:
            print('✅ PushPlus推送成功')
            return True
        else:
            print('❌ PushPlus推送失败: ' + str(result.get('msg', '未知错误')))
            return False
    except Exception as e:
        print('❌ PushPlus推送异常: ' + str(e))
        return False

def should_push():
    """判断是否应该推送（12:00-12:15 和 23:55-23:59 窗口推送，或设置FORCE_PUSH=1强制推送）"""
    if os.getenv('FORCE_PUSH', '') == '1':
        return True
    now = datetime.now()
    if now.hour == 12 and 0 <= now.minute <= 15:
        return True
    if now.hour == 23 and now.minute >= 55:
        return True
    return False

# 当天累计金币存储相关函数
DAILY_GOLD_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'daily_gold_data.json')

def get_daily_gold_data():
    """获取当天累计金币数据"""
    today = datetime.now().strftime('%Y-%m-%d')
    try:
        if os.path.exists(DAILY_GOLD_FILE):
            with open(DAILY_GOLD_FILE, 'r', encoding='utf-8') as f:
                data = json.load(f)
                if data.get('date') == today:
                    return data
        return {'date': today, 'total_gold': 0, 'accounts': {}}
    except Exception:
        return {'date': today, 'total_gold': 0, 'accounts': {}}

def save_daily_gold_data(data):
    """保存当天累计金币数据"""
    try:
        with open(DAILY_GOLD_FILE, 'w', encoding='utf-8') as f:
            json.dump(data, f, ensure_ascii=False, indent=2)
    except Exception as e:
        print('⚠️ 保存累计金币数据失败: ' + str(e))

def update_daily_gold(phone, earned_gold):
    """更新当天累计金币"""
    data = get_daily_gold_data()
    phone_last4 = phone[-4:] if len(phone) > 4 else phone
    
    if phone_last4 not in data['accounts']:
        data['accounts'][phone_last4] = 0
    
    data['accounts'][phone_last4] += earned_gold
    data['total_gold'] += earned_gold
    
    save_daily_gold_data(data)
    return data['accounts'][phone_last4], data['total_gold']


# ======================== 应用启动模拟 ========================
def simulate_app_startup():
    """
    模拟真实应用启动流程
    包含初始化、签名验证、配置加载等
    """
    print("\n📱 模拟应用启动...")
    
    # 1. 验证应用签名
    sig_info = verify_app_signature()
    print(f"  ✓ 应用签名验证: {sig_info['package']}")
    print(f"  ✓ 签名: {sig_info['signature'][:16]}...")
    
    # 2. 加载设备信息
    print(f"  ✓ 设备型号: {DEVICE_MODEL}")
    print(f"  ✓ 系统版本: Android {DEVICE_OS_VERSION}")
    print(f"  ✓ 应用版本: {APP_VERSION} ({APP_VERSION_CODE})")
    
    # 3. 初始化 SDK
    print(f"  ✓ SDK 初始化: {sig_info['sdk']}")
    
    # 4. 模拟启动延迟
    time.sleep(0.5)
    
    print("  ✓ 应用启动完成")
    return sig_info


class OperationLogger:
    """操作日志记录器"""

    def __init__(self):
        self._start_time = time.time()
        self._operations = 0

    def log(self, *args, **kwargs):
        self._operations += 1

    def get_summary(self):
        return {
            "total_operations": self._operations,
            "duration": time.time() - self._start_time,
        }


if __name__ == '__main__':
    # 启动延迟
    stable_delay(1)
    print('============================================================')
    print_banner()
    print('============================================================')
    if not check_expiration():
        exit()
    accounts = get_accounts_from_env()
    if not accounts:
        print('\n❌ 未读取到有效账号，请设置环境变量 kwyy')
        print('格式1: kwyy="手机号#密码"')
        print('格式2: kwyy="备注#手机号#密码"')
        print('多账号: kwyy="手机号1#密码1&手机号2#密码2"')
        exit()
    
    # 初始化操作日志
    _operation_logger = OperationLogger()
    
    _start_time = time.time()
    print('\n📱 获取到 ' + str(len(accounts)) + ' 个账号')
    print(f'🚀 启动时间: {datetime.now().strftime("%Y-%m-%d %H:%M:%S")}')
    
    # 用于收集各账号金币统计
    account_gold_stats = []
    
    for i, account in enumerate(accounts, 1):
        phone = account['phone']
        password = account['password']
        
        # 获取该账号的设备
        device = account.get('device')
        if device is None:
            device = get_device_manager().get_device_for_account(phone)
        
        print('\n' + '==================================================')
        print('👤 账号 ' + str(i) + '/' + str(len(accounts)) + ': ' + str(phone))
        print('📱 设备: #' + str(device['index'] + 1) + ' (' + device['model'] + ')')
        print('==================================================')
        
        # 获取该账号的广告状态
        ad_enabled, ad_reason = get_task_status(phone, 'ad')
        print('📢 广告任务: ' + ad_reason)
        
        encrypted_phone = encrypt_phone(phone)
        q_value, test_encrypted_dev_id = get_q(phone, password)
        print('用户名: ' + str(phone))
        result = login(phone, password)
        
        # 记录该账号起始金币
        start_gold = 0
        end_gold = 0
        total_earned = 0
        
        if result:
            loginUid, loginSid, username, appUid, encrypted_dev_id = result
            print('✅ 登录成功!')
            print('\n【1】任务入口查询:')
            play_entry = fetch_play_entry(loginUid, loginSid, appUid, device=device, verbose=True)
            
            print('\n【2】资产查询:')
            asset_result = query_user_asset(loginUid, loginSid, appUid, verbose=False)
            if asset_result.get('success'):
                start_gold = asset_result.get('score', 0)
                print('  结果: ✅ 剩余金币 ' + str(start_gold))
            else:
                print('  结果: ❌ 查询失败')
            print('\n【3】签到听歌任务链:')
            run_sign_task_chain(loginUid, loginSid, appUid, encrypted_phone, phone=phone)
            
            print('\n【3.5】自动完成任务:')
            auto_result = auto_complete_tasks(loginUid, loginSid, appUid, encrypted_phone, phone=phone, device=device)
            print('\n【4】累计奖励任务:')
            run_coin_accumulation_tasks(loginUid, loginSid, appUid, encrypted_phone)
            print('\n【5】收藏任务:')
            collect_result = run_collect_task(loginUid, loginSid, appUid, encrypted_phone, verbose=False)
            if collect_result['success']:
                if collect_result.get('done'):
                    print('  结果: ⏭️ ' + str(collect_result.get('description', '已完成')))
                else:
                    print('  结果: ✅ +' + str(collect_result.get('obtain', 0)) + ' 金币')
            else:
                print('  结果: ❌ ' + str(collect_result.get('description', '失败')))
            print('\n【6】听书任务:')
            novel_result = run_novel_task(loginUid, loginSid, appUid, encrypted_phone, verbose=False)
            if novel_result['success']:
                if novel_result.get('done'):
                    print('  结果: ⏭️ ' + str(novel_result.get('description', '已完成')))
                else:
                    print('  结果: ✅ +' + str(novel_result.get('obtain', 0)) + ' 金币')
            else:
                print('  结果: ❌ ' + str(novel_result.get('description', '失败')))
            print('\n【8】开宝箱:')
            box_result = open_treasure_box(loginUid, loginSid, appUid, encrypted_dev_id, gold_num=20, verbose=False)
            if box_result['success']:
                print('  结果: ✅ +' + str(box_result.get('obtain', 0)) + ' 金币')
            else:
                print('  结果: ❌ ' + str(box_result['description']))
            print('\n【9】活动宝箱:')
            activity_box_result = run_activity_box_task(loginUid, loginSid, verbose=False)
            if activity_box_result.get('success'):
                if activity_box_result.get('done'):
                    print('  结果: ⏭️ ' + str(activity_box_result.get('description', '已完成')))
                else:
                    print('  结果: ✅ +' + str(activity_box_result.get('obtain', 0)) + ' 金币')
            else:
                print('  结果: ❌ ' + str(activity_box_result.get('description', '失败')))
            print('\n【10】时段宝箱补领:')
            run_box_renew_tasks(loginUid, loginSid, gold_num=30, verbose=True)
            
            # 获取该账号的广告任务状态
            ad_enabled, ad_reason = get_task_status(phone, "ad")
            
            if ad_enabled:
                print('\n【11】看视频拆红包:')
                ad_success_count = 0
                ad_total_gold = 0
                ad_disabled = False
                for j in range(30):
                    # 检查是否被禁用
                    if ad_disabled:
                        print('  第' + str(j+1) + '/30次 ⏭️ 已跳过（错误）')
                        continue
                    
                    ad_result = open_guanggao(loginUid, loginSid, appUid, encrypted_dev_id, 208, encrypted_phone, verbose=False, phone_for_record=phone)
                    round_idx = j + 1
                    if ad_result['success']:
                        obtain = to_int(ad_result.get('obtain') or 0)
                        ad_success_count += 1
                        ad_total_gold += obtain
                        print('  第' + str(round_idx) + '/30次 ✅ +' + str(obtain) + ' 金币')
                    else:
                        # 检测错误
                        is_error, error_info = is_error_response(ad_result)
                        if is_error:
                            ad_disabled = True
                            print(f'  第{round_idx}/30次 ⚠️ 检测到错误: {error_info}，禁用该账号广告功能')
                            print(f'  ⏸️ 广告功能将在 {COOLDOWN_HOURS} 小时后自动恢复')
                            break
                        
                        print('  第' + str(round_idx) + '/30次 ❌ ' + str(ad_result['description']))
                        if is_done_like(ad_result.get('description')):
                            break
                    
                    # 自适应延迟：成功后快速重试，失败后增加等待
                    if ad_result['success']:
                        stable_delay(0.8)
                    else:
                        stable_delay(2.5)
                print('  汇总: 成功 ' + str(ad_success_count) + '/30, 累计 ' + str(ad_total_gold) + ' 金币')
            else:
                print('\n【5】看视频拆红包: ⏭️ 已跳过（' + ad_reason + '）')
            
            print('\n【12】整点领金币:')
            clock_result = clock_bonus(loginUid, loginSid, appUid, encrypted_dev_id, encrypted_phone, verbose=False)
            if clock_result['success']:
                if clock_result.get('done'):
                    print('  结果: ⏭️ ' + str(clock_result.get('description', '已完成')))
                else:
                    print('  结果: ✅ +' + str(clock_result.get('obtain', 0)) + ' 金币')
            else:
                print('  结果: ❌ ' + str(clock_result['description']))
            print('\n【13】免费抽奖:')
            lottery_result = lottery_draw(loginUid, loginSid, appUid, verbose=False)
            if lottery_result['success']:
                reward_name = str(lottery_result.get('reward_name') or lottery_result.get('message') or '成功')
                free_obtain = to_int(lottery_result.get('obtain') or 0)
                if free_obtain > 0:
                    print('  结果: ✅ ' + reward_name + ' (+' + str(free_obtain) + ' 金币)')
                else:
                    print('  结果: ✅ ' + reward_name)
            else:
                print('  结果: ❌ ' + str(lottery_result['message']))
            
            # 重新获取该账号的广告状态（可能因429被禁用）
            ad_enabled, ad_reason = get_task_status(phone, "ad")
            
            if ad_enabled:
                print('\n【14】广告抽奖:')
                lottery_video_success = 0
                lottery_video_total_gold = 0
                for j in range(9):
                    lottery_result = lottery_draw(loginUid, loginSid, appUid, lottery_type='video', verbose=False)
                    round_idx = j + 1
                    if lottery_result['success']:
                        reward_name = str(lottery_result.get('reward_name') or lottery_result.get('message') or '成功')
                        obtain = to_int(lottery_result.get('obtain') or 0)
                        lottery_video_success += 1
                        lottery_video_total_gold += obtain
                        if obtain > 0:
                            print('  第' + str(round_idx) + '/9次 ✅ ' + reward_name + ' (+' + str(obtain) + ' 金币)')
                        else:
                            print('  第' + str(round_idx) + '/9次 ✅ ' + reward_name)
                    else:
                        print('  第' + str(round_idx) + '/9次 ❌ ' + str(lottery_result['message']))
                        if is_done_like(lottery_result.get('message')):
                            break
                    stable_delay(1.5)
                print('  汇总: 成功 ' + str(lottery_video_success) + '/9, 累计 ' + str(lottery_video_total_gold) + ' 金币')
                
                print('\n【15】观看惊喜广告:')
                surprise_success_count = 0
                surprise_total_gold = 0
                for j in range(5):
                    surprise_result = watch_surprise_ad(loginUid, loginSid, appUid, encrypted_dev_id, encrypted_phone, verbose=False)
                    round_idx = j + 1
                    if surprise_result['success']:
                        obtain = to_int(surprise_result.get('obtain') or 0)
                        surprise_success_count += 1
                        surprise_total_gold += obtain
                        print('  第' + str(round_idx) + '/5次 ✅ +' + str(obtain) + ' 金币')
                    else:
                        # 检测错误
                        if '429' in str(surprise_result.get('description', '')):
                            record_task_error(phone, 'ad', 'HTTP 429')
                            print('  第' + str(round_idx) + '/5次 ⚠️ 检测到429错误，禁用该账号广告功能')
                            break
                        # 检测其他错误
                        is_error, error_info = is_error_response(surprise_result)
                        if is_error:
                            record_task_error(phone, 'ad', error_info)
                            print(f'  第{round_idx}/5次 ⚠️ 检测到错误: {error_info}，禁用该账号广告功能')
                            break
                        print('  第' + str(round_idx) + '/5次 ❌ ' + str(surprise_result['description']))
                        if is_done_like(surprise_result.get('description')):
                            break
                    stable_delay(1.5)
                print('  汇总: 成功 ' + str(surprise_success_count) + '/5, 累计 ' + str(surprise_total_gold) + ' 金币')
            else:
                print('\n【9】广告抽奖: ⏭️ 已跳过（' + ad_reason + '）')
                print('\n【10】观看惊喜广告: ⏭️ 已跳过（' + ad_reason + '）')
            print('\n【16】免费听歌时长任务:')
            run_freemium_watch(loginUid, verbose=True)
            
            # 再次查询金币
            final_asset = query_user_asset(loginUid, loginSid, appUid, verbose=False)
            if final_asset.get('success'):
                end_gold = final_asset.get('score', 0)
                total_earned = end_gold - start_gold
                print('\n  📊 本次任务金币变化: ' + str(start_gold) + ' -> ' + str(end_gold) + ' (+' + str(total_earned) + ')')
            
            # 收集统计信息
            account_gold_stats.append({
                'phone': phone[-4:] if len(phone) > 4 else phone,
                'start_gold': start_gold,
                'end_gold': end_gold,
                'earned': total_earned
            })
            
            # 更新当天累计金币
            if total_earned > 0:
                acc_today, total_today = update_daily_gold(phone, total_earned)
                print(f'  📊 当天累计: 账号 +{acc_today}, 全部 +{total_today}')
        else:
            print('❌ 登录失败，跳过后续测试')
            account_gold_stats.append({
                'phone': phone[-4:] if len(phone) > 4 else phone,
                'start_gold': 0,
                'end_gold': 0,
                'earned': 0,
                'error': '登录失败'
            })
        if i < len(accounts):
            print('\n⏳ 账号切换等待 10 秒...')
            stable_delay(1.5)
    print('\n============================================================')
    print('全部账号任务执行结束')
    # 输出操作摘要
    summary = _operation_logger.get_summary()
    print(f'\n📊 操作摘要:')
    print(f'   总操作数: {summary["total_operations"]}')
    print(f'   总耗时: {summary["duration"]:.1f} 秒')
    
    print(f'\n总耗时: {int(time.time() - _start_time)} 秒')
    print('============================================================')
    
    # 显示每个账号统计总数
    print('\n============================================================')
    print('📊 各账号统计汇总')
    print('============================================================')
    now = datetime.now()
    print(f'📅 统计时间: {now.strftime("%Y-%m-%d %H:%M")}')
    print('')
    
    total_all_gold = 0
    total_earned_all = 0
    
    # 获取当天累计金币数据
    daily_data = get_daily_gold_data()
    
    for idx, stat in enumerate(account_gold_stats, 1):
        print(f'📱 账号{idx} ({stat["phone"]}):')
        if 'error' in stat:
            print(f'   ❌ {stat["error"]}')
        else:
            print(f'   起始金币: {stat["start_gold"]}')
            print(f'   当前金币: {stat["end_gold"]}')
            print(f'   本次获得: +{stat["earned"]}')
            # 显示该账号当天累计
            acc_today = daily_data.get('accounts', {}).get(stat["phone"], 0)
            if acc_today > 0:
                print(f'   当天累计: +{acc_today}')
        total_all_gold += stat.get('end_gold', 0)
        total_earned_all += stat.get('earned', 0)
        print('')
    
    print('=' * 40)
    print(f'💰 全部账号金币总计: {total_all_gold}')
    print(f'📈 全部账号本次获得: +{total_earned_all}')
    
    # 显示当天累计金币
    daily_data = get_daily_gold_data()
    if daily_data['total_gold'] > 0:
        print('')
        print(f'🌟 当天累计获得: +{daily_data["total_gold"]}')
        print('📊 各账号当天累计:')
        for acc_phone, acc_gold in daily_data.get('accounts', {}).items():
            print(f'   账号{acc_phone}: +{acc_gold}')
    print('============================================================')
    
    # 推送统计结果
    if should_push() and account_gold_stats:
        print('\n📤 正在推送金币统计到PushPlus...')
        now = datetime.now()
        title = f'酷我音乐金币统计 {now.strftime("%Y-%m-%d")}'
        
        content_lines = [f'📅 统计时间: {now.strftime("%Y-%m-%d %H:%M")}', '']
        total_all_gold = 0
        total_earned_all = 0
        
        # 获取当天累计金币数据
        daily_data = get_daily_gold_data()
        
        for idx, stat in enumerate(account_gold_stats, 1):
            content_lines.append(f'📱 账号{idx} ({stat["phone"]}):')
            if 'error' in stat:
                content_lines.append(f'   ❌ {stat["error"]}')
            else:
                content_lines.append(f'   起始金币: {stat["start_gold"]}')
                content_lines.append(f'   当前金币: {stat["end_gold"]}')
                content_lines.append(f'   本次获得: +{stat["earned"]}')
                # 显示该账号当天累计
                acc_today = daily_data.get('accounts', {}).get(stat["phone"], 0)
                if acc_today > 0:
                    content_lines.append(f'   当天累计: +{acc_today}')
            total_all_gold += stat.get('end_gold', 0)
            total_earned_all += stat.get('earned', 0)
            content_lines.append('')
        
        content_lines.append('=' * 30)
        content_lines.append(f'💰 全部账号金币总计: {total_all_gold}')
        content_lines.append(f'📈 全部账号本次获得: +{total_earned_all}')
        
        # 添加当天累计金币统计
        if daily_data['total_gold'] > 0:
            content_lines.append(f'🌟 当天累计获得: +{daily_data["total_gold"]}')
        
        content = '\n'.join(content_lines)
        push_plus_send(title, content)
