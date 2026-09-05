import { useCallback, useEffect, useRef, useState } from 'react';
import { Button, InputNumber, Modal, Select, Switch, message } from 'antd';
import { HttpUtil, SizeFormatter } from '@/utils';

// 强制以 JSON 发送，避免被全局 axios 默认的 form-urlencoded 编码干扰。
const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } };

interface ApiMsg<T = unknown> {
  success?: boolean;
  msg?: string;
  obj?: T;
}

// 当前累计值（字节）。由 IndexPage 传入，弹窗打开时预填。
export interface CalibrateCurrent {
  sent: number;
  recv: number;
  upTotal: number;
  downTotal: number;
}

const UNITS = [
  { value: 1024, label: 'KB' },
  { value: 1024 ** 2, label: 'MB' },
  { value: 1024 ** 3, label: 'GB' },
  { value: 1024 ** 4, label: 'TB' },
];

// 按 1024 进制挑一个读起来顺手的单位，用于打开弹窗时预填数值。
function pickUnit(bytes: number): { value: number; unit: number } {
  if (!bytes || bytes < 1024) return { value: 0, unit: 1024 ** 3 };
  let unit = 1024;
  for (const u of UNITS) {
    if (bytes >= u.value) unit = u.value;
  }
  return { value: Math.round((bytes / unit) * 100) / 100, unit };
}

type Field = { value: number; unit: number };

// 流量校准：把「当前显示的累计值」改成实际值，之后从新值继续累加。
//
// 网卡计数器是内核级的，无法改写，所以「总数据」卡片用基线偏移法校正：
//   显示值 = 网卡原始读数 − 基线   ⇒   新基线 = 原始读数 − 目标值
// Xray 代理流量是数据库计数（inbounds.up/down 求和），直接按比例缩放到目标值。
export default function SaesonTrafficCalibrateModal({
  open,
  onOpenChange,
  current,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  current: CalibrateCurrent;
}) {
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState<Field>({ value: 0, unit: 1024 ** 3 });
  const [recv, setRecv] = useState<Field>({ value: 0, unit: 1024 ** 3 });
  const [xrayUp, setXrayUp] = useState<Field>({ value: 0, unit: 1024 ** 3 });
  const [xrayDown, setXrayDown] = useState<Field>({ value: 0, unit: 1024 ** 3 });
  const [touchXray, setTouchXray] = useState(false);

  // 每次打开都按最新统计值预填一次，用户只需把数字改成实际值。
  // 注意：IndexPage 每 2 秒轮询一次统计，current 会持续变化，所以必须用 ref
  // 守卫只预填一次，否则用户正在输入的数字会被后台刷新覆盖掉。
  const prefilled = useRef(false);
  useEffect(() => {
    if (!open) {
      prefilled.current = false;
      return;
    }
    if (prefilled.current) return;
    prefilled.current = true;
    setSent(pickUnit(current.sent));
    setRecv(pickUnit(current.recv));
    setXrayUp(pickUnit(current.upTotal));
    setXrayDown(pickUnit(current.downTotal));
    setTouchXray(false);
  }, [open, current.sent, current.recv, current.upTotal, current.downTotal]);

  const toBytes = useCallback((f: Field) => Math.max(0, Math.round((f.value || 0) * f.unit)), []);

  const save = async () => {
    setBusy(true);
    try {
      const msg = (await HttpUtil.post(
        '/panel/api/traffic-reset/calibrate',
        {
          sent: toBytes(sent),
          recv: toBytes(recv),
          xrayUp: touchXray ? toBytes(xrayUp) : null,
          xrayDown: touchXray ? toBytes(xrayDown) : null,
        },
        JSON_HEADERS,
      )) as ApiMsg;
      if (msg?.success) {
        message.success('已校准流量计数，将从新数值继续累计');
        onOpenChange(false);
      } else {
        message.error(msg?.msg || '校准失败');
      }
    } catch {
      message.error('网络错误，请重试');
    } finally {
      setBusy(false);
    }
  };

  const row = (
    label: string,
    val: Field,
    setVal: (f: Field) => void,
    currentBytes: number,
  ) => (
    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
      <span style={{ width: 32 }}>{label}</span>
      <InputNumber
        style={{ width: 130 }}
        min={0}
        step={1}
        value={val.value}
        onChange={(v) => setVal({ ...val, value: Number(v) || 0 })}
      />
      <Select
        style={{ width: 78 }}
        value={val.unit}
        options={UNITS}
        onChange={(u) => setVal({ ...val, unit: u })}
      />
      <span style={{ opacity: 0.55, fontSize: 12, marginLeft: 'auto' }}>
        当前 {SizeFormatter.sizeFormat(currentBytes)}
      </span>
    </div>
  );

  return (
    <Modal
      open={open}
      title="流量校准"
      footer={null}
      width={520}
      centered
      onCancel={() => onOpenChange(false)}
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 14, padding: '8px 0' }}>
        <div style={{ fontWeight: 600 }}>总数据（网卡 · VPS 计费口径）</div>
        {row('上行', sent, setSent, current.sent)}
        {row('下行', recv, setRecv, current.recv)}

        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <span>同时校准「Xray 代理流量」</span>
          <Switch checked={touchXray} onChange={setTouchXray} />
        </div>

        {touchXray && (
          <>
            <div style={{ fontWeight: 600 }}>Xray 代理流量（面板记账）</div>
            {row('上行', xrayUp, setXrayUp, current.upTotal)}
            {row('下行', xrayDown, setXrayDown, current.downTotal)}
          </>
        )}

        <div style={{ opacity: 0.6, fontSize: 12, lineHeight: 1.7 }}>
          填写<b>实际数值</b>，保存后卡片会立刻变成该数值，并从这里继续累加。
          例：面板显示 200 GB 而 VPS 商后台是 30 GB，就把上行填成 30 GB。
          <br />
          总数据采用基线偏移校正（内核网卡计数器无法清零，改的是统计起点，不影响真实流量）；
          Xray 代理流量会按各入站现有占比等比缩放，总量精确等于填写值。
        </div>

        <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
          <Button onClick={() => onOpenChange(false)}>取消</Button>
          <Button type="primary" loading={busy} onClick={save}>
            保存校准
          </Button>
        </div>
      </div>
    </Modal>
  );
}
