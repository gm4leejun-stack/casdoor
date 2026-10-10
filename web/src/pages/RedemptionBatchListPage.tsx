import {useEffect, useState} from "react";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {Card, CardContent, CardHeader, CardTitle} from "@/components/ui/card";
import {useOrganizationFilter} from "@/hooks/use-organization";
import * as RedemptionBackend from "@/backend/RedemptionBackend";
import * as Setting from "@/lib/setting";

export default function RedemptionBatchListPage() {
  const owner = useOrganizationFilter();
  const [batches, setBatches] = useState<any[]>([]);
  const [plan, setPlan] = useState("");
  const [durationDays, setDurationDays] = useState("30");
  const [quantity, setQuantity] = useState("10");
  const load = () => RedemptionBackend.getBatches(owner).then(result => setBatches(result.data ?? []));
  useEffect(() => { if (owner) void load(); }, [owner]);
  const create = async () => {
    const result = await RedemptionBackend.createBatch({owner, name: `batch_${Date.now()}`, displayName: `${plan} codes`, plan, durationDays: Number(durationDays), quantity: Number(quantity)});
    if (result.status !== "ok") { Setting.showMessage("error", result.msg); return; }
    const plaintext = (result.data.codes ?? []).map((row: any) => row.code).join("\n");
    await navigator.clipboard.writeText(plaintext);
    Setting.showMessage("success", "Codes copied once to clipboard. Store them securely.");
    await load();
  };
  return <Card className="m-6">
    <CardHeader><CardTitle>Membership redemption batches</CardTitle></CardHeader>
    <CardContent className="space-y-4">
      <div className="flex gap-2"><Input placeholder="Plan" value={plan} onChange={e => setPlan(e.target.value)} /><Input placeholder="Days" value={durationDays} onChange={e => setDurationDays(e.target.value)} /><Input placeholder="Quantity" value={quantity} onChange={e => setQuantity(e.target.value)} /><Button onClick={create} disabled={!plan}>Generate</Button></div>
      <div className="space-y-2">{batches.map(batch => <div key={`${batch.owner}/${batch.name}`} className="flex items-center justify-between border p-3"><span>{batch.displayName || batch.name} · {batch.plan} · {batch.durationDays} days · {batch.state}</span><Button variant="outline" onClick={async () => { await RedemptionBackend.disableBatch(batch.owner, batch.name); await load(); }}>Disable</Button></div>)}</div>
    </CardContent>
  </Card>;
}
