import { useEffect, useMemo, useState } from 'react';
import {
  Card,
  Col,
  Segmented,
  Select,
  Statistic,
  Tooltip,
  Empty,
} from 'antd';
import {
  ArrowUpOutlined,
  ArrowDownOutlined,
  BarChartOutlined,
  QuestionCircleOutlined,
} from '@ant-design/icons';

import { HttpUtil, SizeFormatter } from '@/utils';

interface Targets {
  inbounds: { id: number; remark: string }[];
  clients: { email: string }[];
}
interface Series {
  labels: string[];
  up: number[];
  down: number[];
  totalUp: number;
  totalDown: number;
}

type Scope = 'inbound' | 'client';
type RangeKey = '24h' | '7d' | '1mo' | '1yr';

const RANGE_OPTIONS: { label: string; value: RangeKey }[] = [
  { label: '24小时', value: '24h' },
  { label: '7天', value: '7d' },
  { label: '1月', value: '1mo' },
  { label: '1年', value: '1yr' },
];

const UP_COLOR = '#1677ff';
const DOWN_COLOR = '#52c41a';

export default function SaesonTrafficHistoryCard() {
  const [targets, setTargets] = useState<Targets | null>(null);
  const [scope, setScope] = useState<Scope>('inbound');
  const [ref, setRef] = useState<string>('');
  const [range, setRange] = useState<RangeKey>('24h');
  const [series, setSeries] = useState<Series | null>(null);
  const [loading, setLoading] = useState(false);

  // load targets once
  useEffect(() => {
    HttpUtil.get<Targets>('/panel/api/traffic/targets')
      .then((msg) => {
        if (msg?.success && msg.obj) setTargets(msg.obj);
      })
      .catch(() => {});
  }, []);

  // pick a default ref when targets arrive / scope changes
  useEffect(() => {
    if (!targets) return;
    if (scope === 'inbound' && targets.inbounds.length) {
      setRef((r) => (r && targets.inbounds.some((x) => String(x.id) === r) ? r : String(targets.inbounds[0].id)));
    } else if (scope === 'client' && targets.clients.length) {
      setRef((r) => (r && targets.clients.some((x) => x.email === r) ? r : targets.clients[0].email));
    } else {
      setRef('');
    }
  }, [targets, scope]);

  // load history when selection changes
  useEffect(() => {
    if (!ref) return;
    setLoading(true);
    HttpUtil.get<Series>(`/panel/api/traffic/history?scope=${scope}&ref=${encodeURIComponent(ref)}&range=${range}`)
      .then((msg) => {
        if (msg?.success && msg.obj) setSeries(msg.obj);
        else setSeries(null);
      })
      .catch(() => setSeries(null))
      .finally(() => setLoading(false));
  }, [scope, ref, range]);

  const options = useMemo(() => {
    if (!targets) return [];
    if (scope === 'inbound') {
      return targets.inbounds.map((x) => ({
        value: String(x.id),
        label: x.remark ? `${x.remark} (#${x.id})` : `#${x.id}`,
      }));
    }
    return targets.clients.map((x) => ({ value: x.email, label: x.email }));
  }, [targets, scope]);

  const title =
    scope === 'inbound'
      ? targets?.inbounds.find((x) => String(x.id) === ref)?.remark || `节点 #${ref}`
      : ref;

  return (
    <Col xs={24}>
      <Card
        title={
          <span>
            <BarChartOutlined /> &nbsp;流量时段统计&nbsp;
            <Tooltip title="按小时采样节点/用户累计流量并存储差值，支持查看 24小时内 / 7天内 / 1月内 / 1年内用量（Xray 重启造成的计数器清零会显示为静默时段）">
              <QuestionCircleOutlined style={{ opacity: 0.5 }} />
            </Tooltip>
          </span>
        }
        hoverable
      >
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 12, alignItems: 'center', marginBottom: 12 }}>
          <Segmented
            value={scope}
            onChange={(v) => setScope(v as Scope)}
            options={[
              { label: '节点', value: 'inbound' },
              { label: '用户', value: 'client' },
            ]}
          />
          <Select
            style={{ minWidth: 220 }}
            value={ref || undefined}
            placeholder={scope === 'inbound' ? '选择节点' : '选择用户'}
            options={options}
            onChange={(v) => setRef(v as string)}
            showSearch
            optionFilterProp="label"
          />
          <Segmented value={range} onChange={(v) => setRange(v as RangeKey)} options={RANGE_OPTIONS} />
        </div>

        <div style={{ display: 'flex', gap: 24, marginBottom: 8 }}>
          <Statistic
            title="↑ 上传累计"
            value={SizeFormatter.sizeFormat(series?.totalUp || 0)}
            prefix={<ArrowUpOutlined style={{ color: UP_COLOR }} />}
          />
          <Statistic
            title="↓ 下载累计"
            value={SizeFormatter.sizeFormat(series?.totalDown || 0)}
            prefix={<ArrowDownOutlined style={{ color: DOWN_COLOR }} />}
          />
          <Statistic
            title="总流量"
            value={SizeFormatter.sizeFormat((series?.totalUp || 0) + (series?.totalDown || 0))}
          />
        </div>

        {series && series.labels.length > 0 ? (
          <TrafficBars series={series} />
        ) : (
          <Empty description={loading ? '加载中…' : '暂无数据（采样器启动后约 5 分钟产生首条记录）'} />
        )}
      </Card>
    </Col>
  );
}

function TrafficBars({ series }: { series: Series }) {
  const n = series.labels.length;
  const max = Math.max(1, ...series.up, ...series.down);
  const chartH = 150;
  const labelH = 18;
  const padTop = 8;
  const groupW = n > 20 ? 26 : 40;
  const barW = groupW > 30 ? 12 : 9;
  const W = Math.max(n * groupW, 280);
  const H = chartH + labelH + padTop;

  // show only a subset of x labels to avoid crowding
  const labelStep = n > 24 ? Math.ceil(n / 12) : n > 12 ? Math.ceil(n / 8) : 1;

  return (
    <div style={{ overflowX: 'auto' }}>
      <div style={{ display: 'flex', gap: 16, fontSize: 12, color: '#888', marginBottom: 4 }}>
        <span><i style={{ display: 'inline-block', width: 10, height: 10, background: UP_COLOR, marginRight: 4 }} />上传</span>
        <span><i style={{ display: 'inline-block', width: 10, height: 10, background: DOWN_COLOR, marginRight: 4 }} />下载</span>
      </div>
      <svg viewBox={`0 0 ${W} ${H}`} width="100%" height={H} preserveAspectRatio="xMinYMin meet">
        {/* baseline */}
        <line x1={0} y1={padTop + chartH} x2={W} y2={padTop + chartH} stroke="#ddd" strokeWidth={1} />
        {series.labels.map((lab, i) => {
          const x = i * groupW;
          const upH = (series.up[i] / max) * chartH;
          const dnH = (series.down[i] / max) * chartH;
          const baseY = padTop + chartH;
          return (
            <g key={i}>
              <rect x={x} y={baseY - upH} width={barW} height={upH} fill={UP_COLOR} opacity={0.85} />
              <rect x={x + barW + 2} y={baseY - dnH} width={barW} height={dnH} fill={DOWN_COLOR} opacity={0.85} />
              {i % labelStep === 0 && (
                <text x={x + groupW / 2} y={baseY + labelH} fontSize={10} fill="#999" textAnchor="middle">
                  {lab}
                </text>
              )}
            </g>
          );
        })}
      </svg>
    </div>
  );
}
