import {Fragment, useState} from "react";
import type {FormEvent} from "react";
import {Link} from "react-router-dom";
import {Button} from "@/components/ui/button";
import {Card, CardContent, CardHeader, CardTitle} from "@/components/ui/card";
import {Input} from "@/components/ui/input";
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow} from "@/components/ui/table";
import {UnauthorizedPage} from "@/components/common/UnauthorizedPage";
import {useAccount} from "@/hooks/use-account";
import * as MembershipBackend from "@/backend/MembershipBackend";
import type {MembershipHistoryField} from "@/backend/MembershipBackend";
import * as Setting from "@/lib/setting";

interface MembershipOrder {
  orderNo: string;
  periodTime: string;
  status: string;
  granted: boolean;
  startsAt?: string;
  expiresAt?: string;
  originalStartsAt?: string;
  originalExpiresAt?: string;
  refundAt?: string;
}

interface MembershipSnapshot {
  startsAt: string;
  expiresAt: string;
  baseStartsAt: string;
  baseExpiresAt: string;
  orders: MembershipOrder[];
}

interface MembershipOperation {
  sequence: number;
  createdTime: string;
  orderNo: string;
  action: string;
  reason: string;
  input: {productType: string; productCode: string; periodTime: string; orderStatus: string};
  before: MembershipSnapshot;
  after: MembershipSnapshot;
}

interface MembershipHistory {
  membershipInitialized: boolean;
  customer: {userId: string; userName: string; mobile: string; uniqueId: string};
  subscription: {startTime: string; endTime: string; state: string} | null;
  orders: MembershipOrder[];
  operations: MembershipOperation[];
  consistencyError?: string;
}

const dateFormatter = new Intl.DateTimeFormat("zh-CN", {
  timeZone: "Asia/Shanghai",
  year: "numeric", month: "2-digit", day: "2-digit",
  hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false,
});

function formatDate(value?: string) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : dateFormatter.format(date);
}

function orderStatus(order?: MembershipOrder) {
  if (!order) return "无记录";
  if (order.status === "6") return order.granted ? "已退款" : "已退款（未发放）";
  return order.status === "2" ? "已开通" : order.status;
}

function actionLabel(action: string) {
  const labels: Record<string, string> = {initialize: "原有会员初始化", initialization: "原有会员初始化", open: "订单开通", grant: "订单开通", refund: "订单退款"};
  return labels[action.toLowerCase()] ?? action;
}

function OrderChanges({operation}: {operation: MembershipOperation}) {
  const before = new Map((operation.before.orders ?? []).map(order => [order.orderNo, order]));
  const changes = (operation.after.orders ?? []).filter(order =>
    JSON.stringify(before.get(order.orderNo)) !== JSON.stringify(order),
  );
  return (
    <div className="space-y-3 py-2">
      <p className="whitespace-normal">{operation.reason}</p>
      {operation.orderNo && <p>商品类型：{operation.input.productType}；商品编码：{operation.input.productCode}；通知周期（退款不采用）：{operation.input.periodTime || "未传"}；通知状态：{operation.input.orderStatus}</p>}
      <p>会员开始时间：{formatDate(operation.before.startsAt)} → {formatDate(operation.after.startsAt)}</p>
      <p>受保护原有会员：{formatDate(operation.after.baseStartsAt)} → {formatDate(operation.after.baseExpiresAt)}</p>
      {changes.length > 0 && (
        <Table>
          <TableHeader><TableRow><TableHead>受影响订单</TableHead><TableHead>状态变化</TableHead><TableHead>原发放周期</TableHead><TableHead>操作前时段</TableHead><TableHead>操作后时段</TableHead></TableRow></TableHeader>
          <TableBody>{changes.map(order => (
            <TableRow key={order.orderNo}>
              <TableCell>{order.orderNo}</TableCell>
              <TableCell>{orderStatus(before.get(order.orderNo))} → {orderStatus(order)}</TableCell>
              <TableCell>{order.granted ? `${order.periodTime}个月` : "未发放"}</TableCell>
              <TableCell>{formatDate(before.get(order.orderNo)?.startsAt)} → {formatDate(before.get(order.orderNo)?.expiresAt)}</TableCell>
              <TableCell>{formatDate(order.startsAt)} → {formatDate(order.expiresAt)}</TableCell>
            </TableRow>
          ))}</TableBody>
        </Table>
      )}
    </div>
  );
}

export default function MembershipHistoryPage() {
  const {account} = useAccount();
  const [field, setField] = useState<MembershipHistoryField>("uniqueId");
  const [candidates, setCandidates] = useState<MembershipHistory["customer"][]>([]);
  const [value, setValue] = useState("");
  const [history, setHistory] = useState<MembershipHistory | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [expanded, setExpanded] = useState<number | null>(null);
  const authorized = !account?.baseMode && (Setting.isAdminUser(account) || account?.isAdmin === true);

  async function search(event: FormEvent) {
    event.preventDefault();
    await loadHistory(field, value);
  }

  async function loadHistory(searchField: MembershipHistoryField, searchValue: string) {
    if (!searchValue.trim() || loading) return;
    setHistory(null);
    setCandidates([]);
    setError("");
    setExpanded(null);
    setLoading(true);
    try {
      const result = await MembershipBackend.getMembershipCustomerHistory(searchField, searchValue.trim());
      if (result.status !== "ok") {
        setError(result.msg || "查询失败");
      } else if (!result.data) {
        setError("未找到会员操作档");
      } else if (result.data.candidates?.length) {
        setCandidates(result.data.candidates);
      } else {
        setHistory({
          ...result.data,
          orders: result.data.orders ?? [],
          operations: [...(result.data.operations ?? [])].sort((a, b) => a.sequence - b.sequence),
        });
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "查询失败");
    } finally {
      setLoading(false);
    }
  }

  if (!authorized) return <UnauthorizedPage />;

  return (
    <div className="space-y-6 p-6">
      <Card>
        <CardHeader><CardTitle>客户会员操作档</CardTitle></CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-muted-foreground">固定套餐 beibei / beibei-pro。按客户操作序号正序展示；所有时间为北京时间。此页只读，不修改会员。</p>
          <form onSubmit={search} className="flex flex-wrap gap-2">
            <select aria-label="查询方式" className="h-9 rounded-md border bg-background px-3 text-sm" value={field} disabled={loading} onChange={e => setField(e.target.value as MembershipHistoryField)}>
              <option value="uniqueId">萨摩耶 uniqueId</option><option value="mobile">历史手机号</option><option value="userId">Casdoor 用户 ID</option><option value="orderNo">萨摩耶订单号</option>
            </select>
            <Input aria-label="查询值" className="w-full sm:w-80" placeholder="请输入完整查询值" value={value} disabled={loading} onChange={e => setValue(e.target.value)} />
            <Button type="submit" disabled={loading || !value.trim()}>{loading ? "查询中…" : "查询"}</Button>
          </form>
          {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
          {candidates.length > 0 && <div className="space-y-2"><p>该历史手机号关联多个客户，请选择 uniqueId 查看，不会自动合并账号：</p>{candidates.map(candidate => <Button key={candidate.userId} variant="outline" disabled={loading} onClick={() => {setField("uniqueId"); setValue(candidate.uniqueId); void loadHistory("uniqueId", candidate.uniqueId);}}>{candidate.uniqueId}（{candidate.userName}）</Button>)}</div>}
        </CardContent>
      </Card>
      {history && (
        <>
          <Card>
            <CardHeader><CardTitle>当前会员</CardTitle></CardHeader>
            <CardContent className="grid gap-2 text-sm sm:grid-cols-2">
              <p>用户：<Link to={"/users/beibei/" + encodeURIComponent(history.customer.userName)} className="underline">{history.customer.userName}</Link></p>
              <p>用户 ID：{history.customer.userId}</p><p>历史绑定手机号：{history.customer.mobile || "未保存"}</p><p>萨摩耶用户标识：{history.customer.uniqueId}</p>
              <p>会员开始：{formatDate(history.subscription?.startTime)}</p><p>会员到期：{formatDate(history.subscription?.endTime)}</p>
              <p>订阅存储状态：{history.subscription?.state || "无订阅"}（是否有效还需结合起止时间）</p><p>操作记录数：{history.operations.length}</p>
              {!history.membershipInitialized && !history.consistencyError && <p className="sm:col-span-2">尚无会员操作档：此账号已建立萨摩耶身份绑定，会员首次维护时才会初始化操作档。</p>}
              {history.consistencyError && <p role="alert" className="text-destructive sm:col-span-2">账实不一致，自动维护已停止：{history.consistencyError}</p>}
            </CardContent>
          </Card>
          <Card>
            <CardHeader><CardTitle>会员变化生命周期</CardTitle></CardHeader>
            <CardContent>
              <Table>
                <TableHeader><TableRow><TableHead>操作序号</TableHead><TableHead>处理时间</TableHead><TableHead>订单号</TableHead><TableHead>操作</TableHead><TableHead>操作前到期</TableHead><TableHead>操作后到期</TableHead><TableHead>详情</TableHead></TableRow></TableHeader>
                <TableBody>{history.operations.map(operation => (
                  <Fragment key={operation.sequence}>
                    <TableRow>
                      <TableCell>{operation.sequence}</TableCell><TableCell>{formatDate(operation.createdTime)}</TableCell><TableCell>{operation.orderNo || "—"}</TableCell>
                      <TableCell>{actionLabel(operation.action)}</TableCell><TableCell>{formatDate(operation.before.expiresAt)}</TableCell><TableCell>{formatDate(operation.after.expiresAt)}</TableCell>
                      <TableCell><Button variant="outline" size="sm" aria-expanded={expanded === operation.sequence} onClick={() => setExpanded(expanded === operation.sequence ? null : operation.sequence)}>{expanded === operation.sequence ? "收起" : "展开"}</Button></TableCell>
                    </TableRow>
                    {expanded === operation.sequence && <TableRow><TableCell colSpan={7}><OrderChanges operation={operation} /></TableCell></TableRow>}
                  </Fragment>
                ))}</TableBody>
              </Table>
            </CardContent>
          </Card>
          <Card>
            <CardHeader><CardTitle>当前订单排期</CardTitle></CardHeader>
            <CardContent>
              <Table>
                <TableHeader><TableRow><TableHead>订单号</TableHead><TableHead>状态</TableHead><TableHead>原发放周期</TableHead><TableHead>最初发放时段</TableHead><TableHead>当前排定时段</TableHead><TableHead>退款处理时间</TableHead></TableRow></TableHeader>
                <TableBody>{history.orders.map(order => (
                  <TableRow key={order.orderNo}>
                    <TableCell>{order.orderNo}</TableCell><TableCell>{orderStatus(order)}</TableCell>
                    <TableCell>{order.granted ? `${order.periodTime}个月` : "未发放"}</TableCell>
                    <TableCell>{formatDate(order.originalStartsAt)} → {formatDate(order.originalExpiresAt)}</TableCell>
                    <TableCell>{formatDate(order.startsAt)} → {formatDate(order.expiresAt)}</TableCell><TableCell>{formatDate(order.refundAt)}</TableCell>
                  </TableRow>
                ))}</TableBody>
              </Table>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
