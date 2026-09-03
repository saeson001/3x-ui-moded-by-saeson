import { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Modal, Select, Typography, message } from 'antd';
import { SelectAllClearButtons } from '@/components/form';
import type { InboundOption } from '@/hooks/useClients';
import { formatInboundLabel } from '@/lib/inbounds/label';
import { HttpUtil } from '@/utils';

// 强制以 JSON 发送，避免被全局 axios 默认的 form-urlencoded 编码干扰。
const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } };

// 只有多用户协议入站才支持关联客户端（与上游 BulkAttachInboundsModal 一致）。
const MULTI_USER_PROTOCOLS = new Set(['vmess', 'vless', 'trojan', 'hysteria', 'shadowsocks']);

interface BulkSetInboundsModalProps {
  open: boolean;
  count: number;
  emails: string[];
  inbounds: InboundOption[];
  onOpenChange: (open: boolean) => void;
}

interface ApiMsg {
  success?: boolean;
  msg?: string;
  obj?: { changed?: number };
}

// 批量「设置关联入站」(replace 语义)：把选中的客户端统一关联到所选入站，
// 解除它们当前关联的其他入站。后端 internal/inboundassoc.Set 实现。
export default function BulkSetInboundsModal({
  open,
  count,
  emails,
  inbounds,
  onOpenChange,
}: BulkSetInboundsModalProps) {
  const [messageApi, messageContextHolder] = message.useMessage();
  const [targetIds, setTargetIds] = useState<number[]>([]);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (open) setTargetIds([]);
  }, [open]);

  const targetOptions = useMemo(() => {
    return (inbounds || [])
      .filter((ib) => MULTI_USER_PROTOCOLS.has((ib.protocol || '').toLowerCase()))
      .map((ib) => ({
        value: ib.id,
        label: formatInboundLabel(ib.tag, ib.remark),
      }));
  }, [inbounds]);

  async function submit() {
    if (targetIds.length === 0 || count === 0) return;
    setSubmitting(true);
    try {
      const msg = (await HttpUtil.post(
        '/panel/api/client-inbounds/bulk-set',
        { emails: emails || [], inboundIds: targetIds },
        JSON_HEADERS,
      )) as ApiMsg;
      if (msg?.success) {
        const changed = msg.obj?.changed ?? count;
        messageApi.success(`已将 ${changed} 个客户端的关联入站设置为所选节点`);
        onOpenChange(false);
      } else {
        messageApi.error(msg?.msg || '设置失败');
      }
    } catch {
      messageApi.error('网络错误，请重试');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <>
      {messageContextHolder}
      <Modal
        open={open}
        title={`设置关联入站（${count} 个客户端）`}
        okText="设置关联入站"
        cancelText="取消"
        okButtonProps={{ disabled: targetIds.length === 0, loading: submitting }}
        onCancel={() => onOpenChange(false)}
        onOk={submit}
        destroyOnHidden
      >
        <Typography.Paragraph type="secondary">
          将选中的 {count} 个客户端的关联入站统一改为所选节点：先解除它们当前关联的其他入站，
          再关联到所选入站（即「全部替换为所选」语义）。
        </Typography.Paragraph>
        {targetOptions.length === 0 ? (
          <Alert type="info" showIcon title="没有可选的多用户入站（vmess/vless/trojan/hysteria/ss）" />
        ) : (
          <>
            <SelectAllClearButtons options={targetOptions} value={targetIds} onChange={setTargetIds} />
            <Select
              mode="multiple"
              style={{ width: '100%' }}
              value={targetIds}
              onChange={setTargetIds}
              options={targetOptions}
              placeholder="选择要关联到的入站节点"
              optionFilterProp="label"
              autoFocus
            />
          </>
        )}
      </Modal>
    </>
  );
}
