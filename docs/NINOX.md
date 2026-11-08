# Schleuse mit Ninox

Es gibt zwei Richtungen. In beiden steht der eigentliche API-Schlüssel nur in Schleuse.

| Richtung | Wer kennt welchen Schlüssel? |
|---|---|
| **Ninox ruft eine fremde API auf** (KI, Shop, CRM, Buchhaltung) | Ninox kennt nur den Schleuse-Token. Den Schlüssel der Ziel-API hinterlegst du in der Schleuse-Verbindung. Ein Ninox-API-Schlüssel ist nicht nötig. |
| **Make, n8n oder ein Skript greift auf Ninox zu** | Der Ninox-API-Schlüssel liegt in der Schleuse-Verbindung „Ninox“. Make kennt nur den Schleuse-Token für die freigegebenen Ninox-Aktionen. |

Wer in Ninox entscheiden darf, wer den Button benutzt, regelst du weiterhin mit den Ninox-Rollen. Schleuse regelt, welche Aktionen der Token ausführen darf.

## Ninox ruft eine API auf

1. In Schleuse die Verbindung aus einer Vorlage anlegen (z. B. **Claude (Anthropic)**, **WooCommerce** oder **HubSpot**), Leseaktion testen und aktivieren.
2. **API-Tokens → Token erstellen**, Aktionen wählen, Laufzeit „unbegrenzt“ lassen.
3. Bei der Aktion auf **Vorlage → Ninox** klicken und das Skript in einen Button unter **Beim Klicken** einfügen. `WERT` durch Felder ersetzen, zum Beispiel `text(Nummer)`.

Lesen mit einem Wert aus dem Datensatz (`GET`, Platzhalter `{id}` im Pfad):

```text
let token := "SCHLEUSE_TOKEN";
let url := "https://api.example.com/actions/kunde-a-woo-bestellung" + "?id=" + urlEncode(text(Bestellnummer));
let response := do as server
    http("GET", url, { Authorization: "Bearer " + token }, null)
end;
let result := response.result;
if response.error or result.error then
    alert("Schleuse-Aufruf fehlgeschlagen: " + text(response))
else
    Status := text(result.status)
end
```

Schreiben mit einer Body-Vorlage (`POST`), zum Beispiel einen Text von Claude erzeugen lassen:

```text
let token := "SCHLEUSE_TOKEN";
let url := "https://api.example.com/actions/kunde-a-claude-text";
let body := {
    text: "Formuliere eine freundliche Zahlungserinnerung für " + Firma
};
let response := do as server
    http("POST", url, { Authorization: "Bearer " + token, "Content-Type": "application/json" }, body)
end;
if response.error then
    alert("Schleuse-Aufruf fehlgeschlagen: " + text(response.error))
else
    let antwort := "";
    for teil in response.result.content do
        if text(teil.type) = "text" then antwort := antwort + text(teil.text) end
    end;
    Entwurf := antwort
end
```

`do as server` sorgt dafür, dass Ninox den Aufruf vom Server aus macht und nicht im Browser. [Ninox-Dokumentation zu HTTP-Aufrufen](https://docs.ninox.com/en/api/http-function)

## Make greift über Schleuse auf Ninox zu

1. In Ninox unter **Einstellungen → Integrationen** einen API-Schlüssel erzeugen.
2. In Schleuse **Aus Vorlage → Ninox** wählen. Als Zieladresse `https://api.ninox.com/v1/teams/<TEAM-ID>/databases/<DATENBANK-ID>` eintragen, bei Ninox On-Premise `https://<dein-server>/v1/teams/…`. Team- und Datenbank-ID stehen in der Browser-Adresse von Ninox. Die Tabellen-ID (A, B, …) liefert die Aktion **Tabellen auflisten**.
3. Die benötigten Aktionen anlegen, testen und aktivieren. Für **Datensatz anlegen** und **Datensatz ändern** die Feldnamen in der Body-Vorlage an deine Tabelle anpassen.
4. Einen Token für diese Aktionen erstellen und bei der Aktion **Vorlage → Make** öffnen.
5. In Make das Modul **HTTP → Make a request** verwenden: URL und Methode aus der Vorlage, Header `Authorization` mit `Bearer <Token>`, bei Schreibaktionen Body-Typ JSON mit den angezeigten Feldern.

Beispiel: Die Aktion **Datensatz anlegen** mit der Vorlage `[{"fields": {"Firma": "{{firma}}", "Ort": "{{ort}}"}}]` erwartet von Make nur:

```json
{"firma": "Beispiel GmbH", "ort": "Berlin"}
```

## Token wechseln

Ein unbegrenzter Token gilt bis zum Widerruf. Zum Wechseln einen neuen Token erstellen, im Skript oder in Make eintragen und danach den alten unter **API-Tokens → Widerrufen** sperren. Ein befristeter Token schlägt nach Ablauf mit `401` fehl.

## Fehlermeldungen verstehen

| Antwort | Bedeutung |
|---|---|
| `401` von Schleuse | Token fehlt, ist widerrufen oder abgelaufen. |
| `403` von Schleuse | Aktion ist für diesen Token nicht freigegeben oder nicht aktiv. |
| `400` von Schleuse | Pflichtwert fehlt, ein Parameter oder Feld ist nicht freigegeben. Die Meldung nennt den Namen. |
| `502` / `504` | Ziel-API nicht erreichbar, Antwort gesperrt oder Zeitlimit von 30 Sekunden erreicht. Details unter **Diagnose**. |
| Andere Codes mit `X-Gateway-Source: upstream` | Antwort der Ziel-API selbst, z. B. `404` für eine unbekannte ID. |
