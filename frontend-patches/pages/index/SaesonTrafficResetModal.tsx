import { useCallback, useEffect, useState } from 'react';
import { Button, InputNumber, Modal, Select, Switch, message, Spin } from 'antd';
import { HttpUtil } from '@/utils';

// 强制以 JSON 发送，避免被全局 axios 默认的 form-urlencoded 编码干扰。
const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } };

interface ResetStatus {
  resetDay: number;
  resetHour: number;
  resetMinute: number;
  enable: boolean;
  lastReset: number;
}

interface ApiMsg<T = unknown> {
  success?: boolean;
  msg?: string;
  obj?: T;
}

const HOURS = Array.from({ length: 24 }, (_, i) => ({ value: i, label: `${String(i).padStart(2, '0')} 时` }));
const MINUTES = Array.from({ length: 60 }, (_, i) => ({ value: i, label: `${String(i).padStart(2, '0')} 分` }));

// 全局流量计数重置（手动 + 定时）：一次性清零「Xray 代理流量」与「总数据」两张
// 卡片。定时沿用 v1.4.0 客户端定时重置的日历日模式，区别是作用域为全局。
export default function SaesonTrafficResetModal({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [enable, setEnable] = useState(true);
  const [day, setDay] = useState(1);
  const [hour, setHour] = useState(0);
  const [minute, setMinute] = useState(0);
  const [lastReset, setLastReset] = useState(0);

  const load = useCallback(async () => {
    if (!open) return;
    setLoading(true);
    try {
      const msg = (await HttpUtil.get('/panel/api/traffic-reset/status')) as ApiMsg<ResetStatus>;
      if (msg?.success && msg.obj) {
        const s = msg.obj;
        setEnable(s.enable);
        setDay(s.resetDay || 1);
        setHour(s.resetHour || 0);
        setMinute(s.resetMinute || 0);
        setLastReset(s.lastReset || 0);
      }
    } catch {
      /* ignore */
    } finally {
      setLoading(false);
    }
  }, [open]);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setBusy(true);
    try {
      const msg = (await HttpUtil.post(
        '/panel/api/traffic-reset/set',
        { resetDay: day, resetHour: hour, resetMinute: minute, enable },
        JSON_HEADERS,
      )) as ApiMsg;
      if (msg?.success) {
        message.success('已保存全局定时重置规则');
        load();
      } else {
        message.error(msg?.msg || '保存失败');
      }
    } finally {
      setBusy(false);
    }
  };

  const resetNow = async () => {
    setBusy(true);
    try {
      const msg = (await HttpUtil.post('/panel/api/traffic-reset/reset-now', {}, JSON_HEADERS)) as ApiMsg;
      if (msg?.success) {
        message.success('已立即重置全部流量计数');
      } else {
        message.error(msg?.msg || '重置失败');
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      open={open}
      title="全局流量计数重置"
      footer={null}
      width={480}
      centered
      onCancel={() => onOpenChange(false)}
    >
      <Spin spinning={loading || busy}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16, padding: '8px 0' }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <span>启用定时重置（每月）</span>
            <Switch checked={enable} onChange={setEnable} />
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
            <span style={{ width: 72 }}>每月</span>
            <InputNumber min={1} max={28} value={day} onChange={(v) => setDay(v || 1)} />
            <span>号</span>
            <Select style={{ width: 110 }} value={hour} options={HOURS} onChange={setHour} />
            <Select style={{ width: 110 }} value={minute} options={MINUTES} onChange={setMinute} />
          </div>

          <div style={{ opacity: 0.6, fontSize: 12, lineHeight: 1.6 }}>
            说明：定时或手动「重置计数」会一次性清零两张卡片 —— Xray 代理流量（面板记账列 + Xray 内存计数器）
            与总数据（网卡累计，采用基线偏移法从 0 重新累计）。同时自动启用被禁用的客户端。
            日期超过当月天数时自动取月末（如 31 在 2 月取 28 日）。
          </div>

          {lastReset > 0 && (
            <div style={{ opacity: 0.6, fontSize: 12 }}>
              上次重置：{new Date(lastReset * 1000).toLocaleString()}
            </div>
          )}

          <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
            <Button onClick={resetNow}>立即重置</Button>
            <Button type="primary" onClick={save}>
              保存
            </Button>
          </div>
        </div>
      </Spin>
    </Modal>
  );
}
