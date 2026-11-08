# Prüfstand von Schleuse 0.3

| Bereich | Geprüfter Stand |
|---|---|
| Automatische Tests | Vue-/TypeScript-Build, Go-Tests mit Race Detector, Installer- und Pakettests. Neu: Pfad-Platzhalter, freigegebene und feste Query-Parameter, Body-Vorlagen mit Typerhalt, Formular-Body, Basic- und URL-Parameter-Anmeldung, Zusatz-Header, Tokens für mehrere Aktionen, unbegrenzte Tokens, Übernahme bestehender Tokens aus 0.2 und die Prüfung aller Konnektor-Vorlagen. |
| Echte Ziel-API | WordPress.org über die Vorlage: Test in der Verwaltung mit HTTP 200 und Datenvorschau, Client-Aufruf mit Token `200`, ohne Token `401`, nicht freigegebener Parameter `400`, fremde Aktion `403`. |
| Ninox und Make | Make → Schleuse → Ninox On-Premise: Lesen eines Datensatzes in Version 0.2, Anlegen (POST) und Ändern (PUT) in Version 0.3 erfolgreich getestet. |
| Oberfläche | Vorlagen-Assistent mit Platzhalter-Prüfung, Testdialog mit Vorschau, Bearbeiten von Verbindungen und Aktionen, Token-Dialog mit Laufzeitwarnung, Client-Vorlagen für Ninox, Make und curl. |

Die Go-Tests verwenden temporäre SQLite-Dateien und lokale Testanbieter. Sie prüfen außerdem Kundentrennung, gesperrte und abgelaufene Zugänge, Admin-CSRF, Zieladressen, Eingabegrenzen und blockierte Schlüsselreflexion.

Offen: Vorlagen mit Status „nach Anbieter-Doku“ mit echten Testkonten prüfen, Ninox Cloud, Lasttests, Restore-Test und eine unabhängige Sicherheitsdurchsicht.
