import { useCallback, useEffect, useState } from 'react';
import { Button, InputNumber, Modal, Select, Switch, message, Spin } from 'antd';
import { HttpUtil } from '@/utils';
import type { ClientRecord } from '@/hooks/useClients';

// 与上游 useClients.ts 中的 JSON_HEADERS 等价：强制以 JSON 发送，
// 避免被全局 axios 默认的 form-urlencoded 编码干扰。
const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } };

interface ClientResetModalProps {
  open: boolean;
  client: ClientRecord | null;
  onOpenChange: (open: boolean) => void;
}

interface Schedule {
  email: string;
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

export default function ClientResetModal({ open, client, onOpenChange }: ClientResetModalProps) {
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [schedule, setSchedule] = useState<Schedule | null>(null);
  const [enable, setEnable] = useState(true);
  const [day, setDay] = useState(1);
  const [hour, setHour] = useState(0);
  const [minute, setMinute] = useState(0);

  const load = useCallback(async () => {
    if (!open || !client?.email) return;
    setLoading(true);
    try {
      const msg = (await HttpUtil.get('/panel/api/client-reset/list')) as ApiMsg<Schedule[]>;
      if (msg?.success && Array.isArray(msg.obj)) {
        const found = msg.obj.find((r) => r.email === client.email) || null;
        setSchedule(found ?? null);
        if (found) {
          setEnable(found.enable);
          setDay(found.resetDay);
          setHour(found.resetHour);
          setMinute(found.resetMinute);
        } else {
          setEnable(true);
          setDay(1);
          setHour(0);
          setMinute(0);
        }
      }
    } catch {
      /* ignore */
    } finally {
      setLoading(false);
    }
  }, [open, client?.email]);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    if (!client?.email) return;
    setBusy(true);
    try {
      const msg = (await HttpUtil.post(
        '/panel/api/client-reset/set',
        { email: client.email, resetDay: day, resetHour: hour, resetMinute: minute, enable },
        JSON_HEADERS,
      )) as ApiMsg;
      if (msg?.success) {
        message.success('已保存定时重置规则');
        load();
      } else {
        message.error(msg?.msg || '保存失败');
      }
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!client?.email) return;
    setBusy(true);
    try {
      const msg = (await HttpUtil.post(
        '/panel/api/client-reset/remove',
        { email: client.email },
        JSON_HEADERS,
      )) as ApiMsg;
      if (msg?.success) {
        message.success('已删除定时重置规则');
        setSchedule(null);
        setEnable(true);
        setDay(1);
        setHour(0);
        setMinute(0);
      } else {
        message.error(msg?.msg || '删除失败');
      }
    } finally {
      setBusy(false);
    }
  };

  const resetNow = async () => {
    if (!client?.email) return;
    setBusy(true);
    try {
      const msg = (await HttpUtil.post(
        '/panel/api/client-reset/reset-now',
        { email: client.email },
        JSON_HEADERS,
      )) as ApiMsg;
      if (msg?.success) {
        message.success('已立即重置流量');
        load();
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
      title={`流量定时重置 — ${client?.email || ''}`}
      footer={null}
      width={480}
      centered
      onCancel={() => onOpenChange(false)}
    >
      <Spin spinning={loading || busy}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16, padding: '8px 0' }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <span>启用定时重置</span>
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
            说明：到达设定的「日期 + 时刻」后，自动将该客户端流量清零。复用面板原生重置逻辑，
            会同时重置 Xray 内存计数器，并自动启用被禁用的客户端。日期超过当月天数时自动取月末（如 31 在 2 月取 28 日）。
          </div>

          {schedule && schedule.lastReset > 0 && (
            <div style={{ opacity: 0.6, fontSize: 12 }}>
              上次重置：{new Date(schedule.lastReset * 1000).toLocaleString()}
            </div>
          )}

          <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
            <Button onClick={resetNow}>立即重置</Button>
            {schedule ? (
              <Button danger onClick={remove}>
                删除规则
              </Button>
            ) : null}
            <Button type="primary" onClick={save}>
              保存
            </Button>
          </div>
        </div>
      </Spin>
    </Modal>
  );
}
