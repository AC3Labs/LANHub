<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>@yield('subject', config('app.name'))</title>
</head>
<body style="margin:0; padding:0; background-color:#f8f7f5; font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Arial,sans-serif;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background-color:#f8f7f5; padding:32px 16px;">
<tr>
<td align="center">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:480px; background-color:#ffffff; border-radius:12px; overflow:hidden; box-shadow:0 1px 3px rgba(44,41,37,0.08);">

<tr>
<td style="padding:28px 32px 20px;">
    <span style="font-size:18px; font-weight:700; color:#2c2925;">{{ config('app.name') }}</span>
</td>
</tr>

<tr>
<td style="background-color:#ac8544; height:3px; line-height:3px; font-size:0;">&nbsp;</td>
</tr>

<tr>
<td style="padding:28px 32px; color:#403c34; font-size:15px; line-height:1.6;">
@yield('content')
</td>
</tr>

<tr>
<td style="background-color:#f8f7f5; padding:18px 32px; border-top:1px solid #e2dfd8;">
    <p style="margin:0; font-size:12px; color:#a8a094;">
        {{ config('app.name') }} &middot; This is an automated message.
    </p>
</td>
</tr>

</table>
</td>
</tr>
</table>
</body>
</html>
