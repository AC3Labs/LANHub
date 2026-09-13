@extends('emails.layout')

@section('subject', 'Your '.config('app.name').' login code')

@section('content')
<p style="margin:0 0 16px;">Someone is trying to log in to your {{ config('app.name') }} account. Enter this code to finish signing in:</p>

<div style="margin:24px 0; padding:18px; background-color:#f8f7f5; border-radius:8px; text-align:center;">
    <span style="font-size:28px; font-weight:700; letter-spacing:4px; color:#2c2925; font-family:'Courier New',monospace;">{{ $code }}</span>
</div>

<p style="margin:0; font-size:13px; color:#867d6e;">This code expires in 10 minutes. If this wasn't you, you can safely ignore this email — your password has not been changed.</p>
@endsection
