import {useState} from "react";
import {Button} from "@/components/ui/button";
import {Input} from "@/components/ui/input";
import {Card, CardContent, CardHeader, CardTitle} from "@/components/ui/card";
import {useOrganizationFilter} from "@/hooks/use-organization";
import * as RedemptionBackend from "@/backend/RedemptionBackend";
import * as Setting from "@/lib/setting";

export default function RedemptionPage() {
  const owner = useOrganizationFilter();
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    try {
      const result = await RedemptionBackend.redeem(owner, code);
      if (result.status !== "ok") throw new Error(result.msg);
      setCode("");
      Setting.showMessage("success", `Membership valid until ${result.data.expiresAt}`);
    } catch (error: any) {
      Setting.showMessage("error", error?.message ?? "Redemption failed");
    } finally { setBusy(false); }
  };

  return <Card className="mx-auto mt-10 max-w-xl">
    <CardHeader><CardTitle>Redeem membership code</CardTitle></CardHeader>
    <CardContent className="flex gap-2">
      <Input value={code} onChange={(event) => setCode(event.target.value)} autoComplete="off" placeholder="Membership code" />
      <Button disabled={busy || !code.trim()} onClick={submit}>Redeem</Button>
    </CardContent>
  </Card>;
}
